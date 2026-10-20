/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/dma"
	spyrert "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/runtime"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	"github.com/ibm-aiu/spyre-operator/controllers/spyrepod"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
)

const (
	FalseIndex             = -1
	MaxAttemptBeforeIgnore = 5
)

type SpyrePodEvent struct {
	EventType
	corev1.Pod
	Attempt int
}

func (se *SpyrePodEvent) Equal(other *SpyrePodEvent) bool {
	return se.EventType == other.EventType &&
		se.Name == other.Name &&
		se.Namespace == other.Namespace
}

type AllocationMemo struct {
	corev1.Pod
	resourceName       string
	allocatedDeviceIDs []string
	mntHostPaths       []string
}

type PodWatcher struct {
	spyreClient *spyreclient.SpyreClient
	*kubernetes.Clientset
	allocatedCh   chan types.AllocationInfo
	mountedCh     chan []string // mounted hostpath
	deallocatedCh chan types.DeallocationInfo
	eventQueue    workqueue.TypedDelayingInterface[*SpyrePodEvent]
	quit          chan struct{}
	memo          map[string]AllocationMemo
	memoLock      sync.RWMutex
}

type EventType string

const (
	AddOrUpdateEvent EventType = "ADD"
	DeleteEvent      EventType = "DELETE"
)

func NewPodWatcher(
	config *rest.Config,
	allocateCh chan types.AllocationInfo,
	mountedCh chan []string,
	deallocatedCh chan types.DeallocationInfo,
) (*PodWatcher, error) {
	var err error
	var clientset *kubernetes.Clientset
	if clientset, err = kubernetes.NewForConfig(config); err == nil {
		spyreClient, err := spyreclient.NewClient(context.Background(), config)
		return &PodWatcher{
			Clientset:     clientset,
			spyreClient:   spyreClient,
			allocatedCh:   allocateCh,
			mountedCh:     mountedCh,
			deallocatedCh: deallocatedCh,
			eventQueue:    workqueue.NewTypedDelayingQueue[*SpyrePodEvent](),
			quit:          make(chan struct{}),
			memo:          make(map[string]AllocationMemo),
		}, err
	}
	return nil, err
}

// Start watching Pod add/update/delete and update corresponding allocation/de-allocation
func (w *PodWatcher) Start() {
	nodeSelector := fields.SelectorFromSet(fields.Set{"spec.nodeName": utils.GetNodeName()})
	glog.Infof("start PodWatcher with selector %v", nodeSelector)
	podListWatcher := cache.NewListWatchFromClient(
		w.Clientset.CoreV1().RESTClient(), string(corev1.ResourcePods), metav1.NamespaceAll, nodeSelector)

	informerOptions := cache.InformerOptions{
		ObjectType:    &corev1.Pod{},
		ListerWatcher: podListWatcher,
		ResyncPeriod:  time.Second * 0,
		Indexers:      cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    w.onPodAdd,
			UpdateFunc: w.onPodUpdate,
			DeleteFunc: w.onPodDelete,
		},
	}
	_, informer := cache.NewInformerWithOptions(informerOptions)

	w.cleanInitialUnmountedDevices()
	w.StartProcessEventQueue(informer)
}

// enqueueEvent logs the message and adds the event of the pod to the event queue.
func (w *PodWatcher) enqueueEvent(eventType EventType, p *corev1.Pod, logPrefix string) {
	glog.V(1).Infof("%s: %s/%s", logPrefix, p.Namespace, p.Name)
	w.eventQueue.Add(&SpyrePodEvent{
		EventType: eventType,
		Pod:       *p,
	})
}

func (w *PodWatcher) onPodAdd(obj interface{}) {
	p, ok := obj.(*corev1.Pod)
	if ok && spyrepod.IsSpyrePod(p) && p.Status.Phase == corev1.PodRunning {
		w.enqueueEvent(AddOrUpdateEvent, p, "add")
	}
}

func (w *PodWatcher) onPodUpdate(obj, newObj interface{}) {
	p, ok := newObj.(*corev1.Pod)
	if !ok || !spyrepod.IsSpyrePod(p) {
		return
	}
	prev := obj.(*corev1.Pod)
	switch {
	case p.Status.Phase == corev1.PodRunning ||
		(p.Status.Phase == corev1.PodPending && allSpyreContainersStarted(p)):
		if _, exist := w.getPodMemo(*p); !exist {
			w.enqueueEvent(AddOrUpdateEvent, p, "update")
		}
	case p.Status.Phase == corev1.PodSucceeded && prev.Status.Phase != corev1.PodSucceeded:
		w.enqueueEvent(DeleteEvent, p, "release on complete")
	case p.Spec.RestartPolicy == corev1.RestartPolicyNever && p.Status.Phase == corev1.PodFailed &&
		prev.Status.Phase != corev1.PodFailed:
		w.enqueueEvent(DeleteEvent, p, "release on error")
	}
}

func (w *PodWatcher) onPodDelete(obj interface{}) {
	p, ok := obj.(*corev1.Pod)
	if ok && spyrepod.IsSpyrePod(p) {
		w.enqueueEvent(DeleteEvent, p, "delete")
	}
}

// cleanInitialUnmountedDevices cleans unmounted host paths left from before start
// and notifies the deallocation of their devices (not in reservation mode).
func (w *PodWatcher) cleanInitialUnmountedDevices() {
	if !spyreconf.IsConfigHostPathExist() || !spyreconf.IsSomeContainerMounted() {
		return
	}
	unmountedDevices, err := spyrert.CleanUnmountedHostPath()
	if err != nil || len(unmountedDevices) == 0 {
		return
	}
	glog.V(1).Infof("deallocate initial unmounted devices: %v", unmountedDevices)
	if utils.IsReservationMode() {
		return
	}
	for resourceName, deviceIDs := range unmountedDevices {
		w.deallocatedCh <- types.DeallocationInfo{
			DeviceIDs:    deviceIDs,
			ResourceName: resourceName,
		}
	}
}

// Run begins watching and syncing.
func (w *PodWatcher) StartProcessEventQueue(informer cache.Controller) {
	go informer.Run(w.quit)
	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(w.quit, informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	go wait.Until(w.runWorker, time.Second, w.quit)
}

func (w *PodWatcher) runWorker() {
	for w.processNextItem() {
	}
}

func (w *PodWatcher) processNextItem() bool {
	ctx := context.Background()
	event, quit := w.eventQueue.Get()
	if quit {
		return false
	}
	defer w.eventQueue.Done(event)
	w.processEvent(ctx, event)
	return true
}

func (w *PodWatcher) processEvent(ctx context.Context, event *SpyrePodEvent) {
	p := event.Pod
	// If the pod is failed to allocate, it will never have a config file or listed in SpyreNodeState's status.
	// Only the pod that is successfully allocated should be processed.
	if event.Attempt > MaxAttemptBeforeIgnore {
		glog.Infof("%s/%s waited too long, ignoring this %s event", p.Namespace, p.Name, event.EventType)
		return
	}
	glog.V(1).Infof("processing %s: %s/%s", event.EventType, p.Namespace, p.Name)
	switch event.EventType {
	case AddOrUpdateEvent:
		w.allocateAndRequeueIfFailed(ctx, event)
	case DeleteEvent:
		w.deallocateAndRequeueIfFailed(ctx, event)
	}
}

// requeue sleeps for n seconds before putting the item back to the queue when
// n = event.Attempt and n is increased every time of requeue.
func (w *PodWatcher) requeue(event *SpyrePodEvent) {
	event.Attempt += 1
	w.eventQueue.AddAfter(event, time.Duration(event.Attempt)*time.Second)
}

// NotifyInitialList must be called once after InitServers
func (w *PodWatcher) NotifyInitialAllocationList() {

	ctx := context.Background()

	fieldSelector := fmt.Sprintf("spec.nodeName=%s", utils.GetNodeName())
	listOptions := metav1.ListOptions{
		FieldSelector: fieldSelector,
	}

	initialPodList, err := w.Clientset.CoreV1().Pods(metav1.NamespaceAll).List(
		context.Background(), listOptions)
	if err == nil {
		allocationList := []spyrev1alpha1.Allocation{}
		allocationInfoList := []types.AllocationInfo{}
		for _, p := range initialPodList.Items {
			if spyrepod.IsSpyrePod(&p) {
				_, allocatedDeviceIDs, mntHostPaths := w.getAndUpdateAllocationMemoIfNotExist(&p)
				if len(allocatedDeviceIDs) > 0 {
					resourceName := utils.GetResourceNameFromPod(&p)
					allocation := spyrev1alpha1.Allocation{
						Pod:          podReference(&p),
						DeviceList:   allocatedDeviceIDs,
						ResourcePool: resourceName,
					}
					allocationList = append(allocationList, allocation)
					allocationInfo := types.AllocationInfo{
						DeviceIDs:    allocatedDeviceIDs,
						MountPoints:  mntHostPaths,
						ResourceName: resourceName,
					}
					allocationInfoList = append(allocationInfoList, allocationInfo)
				}
			}
		}

		glog.Infof("add initial allocation: %v", allocationList)
		if nodeState, err := spyredevice.GetNodeStateForThisNode(ctx, w.spyreClient); err != nil {
			glog.Error("failed to get current node state: ", err)
		} else {
			nodeState.Status.AllocationList = allocationList
			if _, err = w.spyreClient.UpdateStatus(ctx, nodeState, true); err != nil {
				glog.Error("failed to update status")
			}
			if !utils.IsReservationMode() {
				for _, allocationInfo := range allocationInfoList {
					glog.Infof("start push %s", allocationInfo)
					w.allocatedCh <- allocationInfo
					glog.Infof("done push %s", allocationInfo)
				}
			}
		}
	}
}

func (w *PodWatcher) Stop() {
	close(w.quit)
	defer runtime.HandleCrash()
	defer w.eventQueue.ShutDown()
}

// deallocateAndRequeueIfFailed calls deallocate function and requeue on its error.
func (w *PodWatcher) deallocateAndRequeueIfFailed(ctx context.Context, event *SpyrePodEvent) {
	p := event.Pod
	err := w.deallocate(ctx, p)
	if err != nil {
		glog.Infof("failed to deallocate devices from %s/%s: %v, requeue at %d attempt",
			p.Namespace, p.Name, err, event.Attempt)
		if isPodAllocateFailed(p) {
			glog.Infof("pod %s/%s is failed to deallocate, ignore", p.Namespace, p.Name)
			return
		}
		w.requeue(event)
	}
}

// deallocate cleans host-mounted path and sends information of deallocatedDeviceIDs to deallocatedCh.
// if cleaning succeed, remove mntHostPaths from the memo to not repeat the action.
// updates SpyreNodeState by removing the deleted entry.
// if the update succeeds, remove pod entry from the memo.
// if the pod entry is not available in the memo, skip
func (w *PodWatcher) deallocate(ctx context.Context, p corev1.Pod) error {
	memo, foundMemo := w.getPodMemo(p)
	if foundMemo {
		w.cleanMountPathsAndNotify(&memo)
		glog.Infof("successfully deallocate %v by container ID", memo.allocatedDeviceIDs)
	} else {
		glog.Infof("pod %s is not available in the memo, ignore", getPodKey(p))
	}

	w.unsetP2PDMAPermissions(ctx, p)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return w.removeAllocationOrReservation(ctx, p)
	})
	if foundMemo && err == nil {
		w.deleteMemo(p)
	}
	return err
}

// cleanMountPathsAndNotify removes host-mounted paths of the memo
// and sends the deallocation info to deallocatedCh (not in reservation mode).
func (w *PodWatcher) cleanMountPathsAndNotify(memo *AllocationMemo) {
	if len(memo.mntHostPaths) == 0 {
		return
	}
	for _, mntHostPath := range memo.mntHostPaths {
		if err := os.RemoveAll(mntHostPath); err != nil {
			glog.Errorf("failed to remove %s", mntHostPath)
			continue
		}
		glog.Infof("successfully remove unmount of %s", mntHostPath)
		// clean mount path to skip repetition if SpyreNodeState update failed
		memo.mntHostPaths = []string{}
	}
	if !utils.IsReservationMode() {
		w.deallocatedCh <- types.DeallocationInfo{
			DeviceIDs:    memo.allocatedDeviceIDs,
			ResourceName: memo.resourceName,
		}
	}
}

// unsetP2PDMAPermissions unsets device resource file permissions if the pod has an allocation requiring P2P DMA.
func (w *PodWatcher) unsetP2PDMAPermissions(ctx context.Context, p corev1.Pod) {
	_, _, allocation, err := w.getAllocation(ctx, p)
	if err != nil || !dma.NeedP2PDMAConfigure(allocation.DeviceList) {
		return
	}
	if errs := dma.UnsetDevResourceFilePermissions(allocation.DeviceList); len(errs) > 0 {
		glog.Warningf("some device resource file may not be unset: %v", errs)
	}
}

// removeAllocationOrReservation removes the pod allocation from SpyreNodeState.
// If the pod is not in AllocationList, in reservation mode, any pending reservation is cleaned up.
func (w *PodWatcher) removeAllocationOrReservation(ctx context.Context, p corev1.Pod) error {
	nodeState, index, _, err := w.getAllocation(ctx, p)
	if err == nil {
		return w.removeAllocation(ctx, nodeState, index)
	}
	if !utils.IsReservationMode() {
		return nil
	}
	nodeState, getErr := spyredevice.GetNodeStateForThisNode(ctx, w.spyreClient)
	if getErr != nil {
		return nil
	}
	nodeState.Status.Reservations = reconcileReservation(nodeState, podReference(&p), nil)
	_, updateErr := w.spyreClient.UpdateStatus(ctx, nodeState, false)
	return updateErr
}

func (w *PodWatcher) getAllocation(ctx context.Context,
	p corev1.Pod) (*spyrev1alpha1.SpyreNodeState, int, *spyrev1alpha1.Allocation, error) {
	podKey := getPodKey(p)
	nodeState, err := spyredevice.GetNodeStateForThisNode(ctx, w.spyreClient)
	if err != nil {
		return nil, -1, nil, err
	}
	for index, allocation := range nodeState.Status.AllocationList {
		if allocation.Pod != nil && allocation.Pod.SameAs(*podReference(&p)) {
			return nodeState, index, &allocation, nil
		}
	}
	return nil, -1, nil, fmt.Errorf("cannot find allocation %s to remove from spyre-node-state", podKey)
}

func (w *PodWatcher) removeAllocation(ctx context.Context, nodeState *spyrev1alpha1.SpyreNodeState, index int) error {
	newAllocationList := utils.RemoveAllocationIndex(nodeState.Status.AllocationList, index)
	glog.Infof("new allocationList: %v from %v", newAllocationList, nodeState.Status.AllocationList)
	nodeState.Status.AllocationList = newAllocationList
	_, err := w.spyreClient.UpdateStatus(ctx, nodeState, false)
	return err
}

// allocateAndRequeueIfFailed calls allocate and requeue if getting an error.
// ignore if the memo entry has been deleted in the case that DELETE event processed before.
func (w *PodWatcher) allocateAndRequeueIfFailed(ctx context.Context, event *SpyrePodEvent) {
	p := event.Pod
	latestPod, alive := w.isPodAlive(ctx, &p)
	if !alive {
		glog.Infof("pod %s/%s is not alive or deleted, stop requeue for %s event", p.Namespace, p.Name, event.EventType)
		return
	}
	p = *latestPod

	memo, found := w.getPodMemo(p)
	if !found {
		added, _, mntHostPaths := w.getAndUpdateAllocationMemoIfNotExist(&p)
		if !added {
			glog.V(1).Infof("devices not yet ready for %s/%s, requeue at %d attempt",
				p.Namespace, p.Name, event.Attempt)
			w.requeue(event)
			return
		}
		w.mountedCh <- mntHostPaths
		memo, found = w.getPodMemo(p)
		if !found {
			return
		}
	}
	err := w.allocate(ctx, p, memo.allocatedDeviceIDs, memo.resourceName)
	if err != nil {
		glog.Infof("failed to allocate devices from %s/%s: %v, requeue at %d attempt",
			p.Namespace, p.Name, err, event.Attempt)
		w.requeue(event)
	}
}

// isPodAlive returns true and the latest Pod object if the pod still exists, is not marked for deletion,
// and has not reached a terminal phase.
func (w *PodWatcher) isPodAlive(ctx context.Context, p *corev1.Pod) (*corev1.Pod, bool) {
	if p.DeletionTimestamp != nil {
		return nil, false
	}
	if p.Status.Phase == corev1.PodFailed || p.Status.Phase == corev1.PodSucceeded {
		return nil, false
	}
	if w.Clientset == nil {
		return p, true
	}
	latestPod, err := w.Clientset.CoreV1().Pods(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if err != nil {
		return nil, false
	}
	if latestPod.UID != p.UID {
		return nil, false
	}
	if latestPod.DeletionTimestamp != nil {
		return nil, false
	}
	if latestPod.Status.Phase == corev1.PodFailed || latestPod.Status.Phase == corev1.PodSucceeded {
		return nil, false
	}
	return latestPod, true
}

// allocate updates allocatedDeviceIDs in SpyreNodeState if not exists.
func (w *PodWatcher) allocate(
	ctx context.Context, p corev1.Pod, allocatedDeviceIDs []string, resourceName string) error {
	var err error
	var nodeState *spyrev1alpha1.SpyreNodeState

	podRef := podReference(&p)
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if nodeState, err = spyredevice.GetNodeStateForThisNode(ctx, w.spyreClient); err != nil {
			return err
		}
		// Bring Reservation.Entries up to date with whatever the last writer left
		// behind, so the reconciliation below can go by Pod rather than by size.
		nodeState.Status.NormalizeReservations()
		for _, allocation := range nodeState.Status.AllocationList {
			// skip if the item has already added.
			// The generation has to match: a successor Pod that reuses the name of
			// one still being torn down is a different allocation, not this one.
			if allocation.Pod != nil && allocation.Pod.SameAs(*podRef) {
				return nil
			}
		}
		// add new item if not exist
		newAllocation := spyrev1alpha1.Allocation{
			Pod:          podRef,
			DeviceList:   allocatedDeviceIDs,
			ResourcePool: resourceName,
		}
		nodeState.Status.AllocationList = append(nodeState.Status.AllocationList, newAllocation)
		nodeState.Status.Reservations = reconcileReservation(nodeState, podRef, allocatedDeviceIDs)
		glog.Infof("reservation to allocation in allocate (len diff): AllocationList: %v, Reservations: %v",
			nodeState.Status.AllocationList, nodeState.Status.Reservations)
		_, err = w.spyreClient.UpdateStatus(ctx, nodeState, false)
		if err != nil {
			glog.Infof("add new allocation to SpyreNodeState %s/%s (err: %v)", p.Namespace, p.Name, err)
		}
		return err
	})
	return err
}

// Functions to handle AllocationMemo map
// getAndUpdateAllocationMemoIfNotExist gets allocation memo and adds new allocation (device IDs and
// its mounted paths on the host associated to pod) if not exist.
// applied only when getting information of running pod from init and watch function and the number
// of allocatedDeviceIDs > 0 and entry not exists return true if new entry added.
func (w *PodWatcher) getAndUpdateAllocationMemoIfNotExist(p *corev1.Pod) (bool, []string, []string) {
	key := getPodKey(*p)
	memo, exist := w.getPodMemo(*p)
	if exist {
		return false, memo.allocatedDeviceIDs, memo.mntHostPaths
	}
	w.memoLock.Lock()
	defer w.memoLock.Unlock()

	// GetDevicesAndMounts is relatively costly. Only call when needed.
	allocatedDeviceIDs, mntHostPaths, err := spyrert.GetDevicesAndMounts(p)
	if err != nil {
		return false, allocatedDeviceIDs, mntHostPaths
	}
	if len(allocatedDeviceIDs) == 0 {
		glog.V(1).Infof("unexpected allocatedDeviceIDs length zero on pod %s/%s, requeue", p.Namespace, p.Name)
		return false, allocatedDeviceIDs, mntHostPaths
	}
	resourceName := utils.GetResourceNameFromPod(p)
	w.memo[key] = AllocationMemo{
		Pod:                *p,
		resourceName:       resourceName,
		allocatedDeviceIDs: allocatedDeviceIDs,
		mntHostPaths:       mntHostPaths,
	}
	if err = spyreconf.WritePodInfo(mntHostPaths, *p); err != nil {
		glog.Warningf("failed to write pod info %s/%s for exporting metrics: %v", p.Namespace, p.Name, err)
	} else {
		glog.Infof("successfully added %s: %s/%s", key, p.Namespace, p.Name)
	}
	return true, allocatedDeviceIDs, mntHostPaths
}

// getPodMemo returns memo of the pod and found boolean.
func (w *PodWatcher) getPodMemo(p corev1.Pod) (AllocationMemo, bool) {
	w.memoLock.RLock()
	defer w.memoLock.RUnlock()
	key := getPodKey(p)
	allocationMemo, found := w.memo[key]
	return allocationMemo, found
}

// deleteMemo removes pod entry from memo.
// applied only when the allocation is properly deleted from the SpyreNodeState.
func (w *PodWatcher) deleteMemo(p corev1.Pod) {
	w.memoLock.Lock()
	defer w.memoLock.Unlock()
	key := getPodKey(p)
	delete(w.memo, key)
}

func isSpyreContainer(c corev1.Container) bool {
	for res := range c.Resources.Requests {
		if strings.Contains(res.String(), utils.ResourceNamePrefix) {
			return true
		}
	}
	for res := range c.Resources.Limits {
		if strings.Contains(res.String(), utils.ResourceNamePrefix) {
			return true
		}
	}
	return false
}

// allSpyreContainersStarted returns true if every container requesting Spyre devices has started.
func allSpyreContainersStarted(p *corev1.Pod) bool {
	spyreContainers := make(map[string]bool)
	for _, containers := range [][]corev1.Container{p.Spec.Containers, p.Spec.InitContainers} {
		for _, c := range containers {
			if isSpyreContainer(c) {
				spyreContainers[c.Name] = false
			}
		}
	}
	if len(spyreContainers) == 0 {
		return false
	}

	markStartedContainers(spyreContainers, p.Status.ContainerStatuses)
	markStartedContainers(spyreContainers, p.Status.InitContainerStatuses)

	for _, started := range spyreContainers {
		if !started {
			return false
		}
	}
	return true
}

// markStartedContainers sets true to the entries of spyreContainers whose status shows the container has started.
// A status without a name marks all entries.
func markStartedContainers(spyreContainers map[string]bool, statuses []corev1.ContainerStatus) {
	for _, cs := range statuses {
		if cs.ContainerID == "" && cs.State.Running == nil {
			continue
		}
		if cs.Name == "" {
			for name := range spyreContainers {
				spyreContainers[name] = true
			}
		} else if _, exists := spyreContainers[cs.Name]; exists {
			spyreContainers[cs.Name] = true
		}
	}
}

// isPodAllocateFailed returns true if pod is failed with error message Allocate failed.
func isPodAllocateFailed(p corev1.Pod) bool {
	if p.Status.Phase == corev1.PodFailed && strings.Contains(p.Status.Message, "Allocate failed") {
		return true
	}
	return false
}

func getPodKey(pod corev1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

// podReference records which generation of a Pod an allocation or a reservation
// belongs to. The UID is what makes that unambiguous: a runner set recreates Pods
// under the same name every few seconds, so name and namespace alone cannot tell
// a successor apart from the predecessor it replaced.
func podReference(p *corev1.Pod) *spyrev1alpha1.Pod {
	return &spyrev1alpha1.Pod{Name: p.Name, Namespace: p.Namespace, UID: p.UID}
}

// reconcileReservation retires the reservation that has just become an allocation.
//
// The Pod is the authority: once its devices are allocated, the entry recorded for
// that exact Pod generation has served its purpose. Only when no entry names the
// Pod - a reservation written by a scheduler that predates the Pod-bound model, or
// one whose owner the compatibility shim could not recover - does it fall back to
// the device set, which the allocation is definitely made of.
//
// The previous version removed by name and by device set independently, and did so
// with a swap-remove while ranging over the very slice it was shortening, which
// skipped elements and could leave the two lists disagreeing about who held what.
func reconcileReservation(
	nodeState *spyrev1alpha1.SpyreNodeState,
	pod *spyrev1alpha1.Pod,
	allocatedDeviceIDs []string,
) map[string]spyrev1alpha1.Reservation {

	newReservations := make(map[string]spyrev1alpha1.Reservation)
	for resName, reservation := range nodeState.Status.Reservations {
		// Work on a copy: the removals rewrite the entry slice in place, and the
		// caller still holds the original map.
		r := *reservation.DeepCopy()
		if !r.RemoveByPod(*pod) {
			r.RemoveByDevices(allocatedDeviceIDs)
		}
		if !r.IsEmpty() {
			newReservations[resName] = r
		}
	}
	return newReservations
}
