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
	"reflect"
	"sort"
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
		se.Pod.ObjectMeta.Name == other.Pod.ObjectMeta.Name &&
		se.Pod.ObjectMeta.Namespace == other.Pod.ObjectMeta.Namespace
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

func NewPodWatcher(config *rest.Config, allocateCh chan types.AllocationInfo, mountedCh chan []string, deallocatedCh chan types.DeallocationInfo) (*PodWatcher, error) { //nolint:lll
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
			AddFunc: func(obj interface{}) {
				p, ok := obj.(*corev1.Pod)
				if ok && spyrepod.IsSpyrePod(p) && p.Status.Phase == corev1.PodRunning {
					glog.V(1).Infof("add: %s/%s", p.Namespace, p.Name)
					_, _, mntHostPaths := w.getAndUpdateAllocationMemoIfNotExist(p)
					event := &SpyrePodEvent{
						EventType: AddOrUpdateEvent,
						Pod:       *p,
					}
					w.eventQueue.Add(event)
					w.mountedCh <- mntHostPaths
				}
			},
			UpdateFunc: func(obj, newObj interface{}) {
				p, ok := newObj.(*corev1.Pod)
				if !ok || !spyrepod.IsSpyrePod(p) {
					return
				}
				prev := obj.(*corev1.Pod)
				switch {
				case p.Status.Phase == corev1.PodRunning ||
					(p.Status.Phase == corev1.PodPending && len(p.Status.ContainerStatuses) > 0):
					if _, exist := w.getPodMemo(*p); exist {
						return
					}
					added, _, mntHostPaths := w.getAndUpdateAllocationMemoIfNotExist(p)
					if added {
						glog.V(1).Infof("update: %s/%s", p.Namespace, p.Name)
						event := &SpyrePodEvent{
							EventType: AddOrUpdateEvent,
							Pod:       *p,
						}
						w.eventQueue.Add(event)
						w.mountedCh <- mntHostPaths
					}
				case p.Status.Phase == corev1.PodSucceeded && prev.Status.Phase != corev1.PodSucceeded:
					glog.V(1).Infof("release on complete: %s/%s", p.Namespace, p.Name)
					event := &SpyrePodEvent{
						EventType: DeleteEvent,
						Pod:       *p,
					}
					w.eventQueue.Add(event)
				case p.Spec.RestartPolicy == corev1.RestartPolicyNever && p.Status.Phase == corev1.PodFailed &&
					prev.Status.Phase != corev1.PodFailed:
					glog.V(1).Infof("release on error: %s/%s", p.Namespace, p.Name)
					event := &SpyrePodEvent{
						EventType: DeleteEvent,
						Pod:       *p,
					}
					w.eventQueue.Add(event)
				}
			},
			DeleteFunc: func(obj interface{}) {
				p, ok := obj.(*corev1.Pod)
				if ok && spyrepod.IsSpyrePod(p) {
					glog.V(1).Infof("delete: %s/%s", p.Namespace, p.Name)
					event := &SpyrePodEvent{
						EventType: DeleteEvent,
						Pod:       *p,
					}
					w.eventQueue.Add(event)
				}
			},
		},
	}
	_, informer := cache.NewInformerWithOptions(informerOptions)

	if spyreconf.IsConfigHostPathExist() && spyreconf.IsSomeContainerMounted() {
		if unmountedDevices, err := spyrert.CleanUnmountedHostPath(); err == nil && len(unmountedDevices) > 0 {
			glog.V(1).Infof("deallocate initial unmounted devices: %v", unmountedDevices)
			if !utils.IsReservationMode() {
				for resourceName, deviceIDs := range unmountedDevices {
					w.deallocatedCh <- types.DeallocationInfo{
						DeviceIDs:    deviceIDs,
						ResourceName: resourceName,
					}
				}
			}
		}
	}
	w.StartProcessEventQueue(informer)
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

	if initialPodList, err := w.Clientset.CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), listOptions); err == nil { //nolint:lll
		allocationList := []spyrev1alpha1.Allocation{}
		allocationInfoList := []types.AllocationInfo{}
		for _, p := range initialPodList.Items {
			if spyrepod.IsSpyrePod(&p) {
				_, allocatedDeviceIDs, mntHostPaths := w.getAndUpdateAllocationMemoIfNotExist(&p)
				if len(allocatedDeviceIDs) > 0 {
					resourceName := utils.GetResourceNameFromPod(&p)
					allocation := spyrev1alpha1.Allocation{
						Pod: &spyrev1alpha1.Pod{
							Name:      p.Name,
							Namespace: p.Namespace,
						},
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
	podKey := getPodKey(p)
	memo, found := w.getPodMemo(p)
	if !found {
		glog.Infof("pod %s is not available in the memo, ignore", podKey)
		return nil
	}
	deallocatedDeviceIDs := memo.allocatedDeviceIDs
	mntHostPaths := memo.mntHostPaths
	if len(mntHostPaths) > 0 {
		// remove mount path
		for _, mntHostPath := range mntHostPaths {
			if err := os.RemoveAll(mntHostPath); err != nil {
				glog.Errorf("failed to remove %s", mntHostPath)
			} else {
				glog.Infof("successfully remove unmount of %s", mntHostPath)
				// clean mount path to skip repetition if SpyreNodeState update failed
				memo.mntHostPaths = []string{}
			}
		}
		if !utils.IsReservationMode() {
			w.deallocatedCh <- types.DeallocationInfo{
				DeviceIDs:    deallocatedDeviceIDs,
				ResourceName: memo.resourceName,
			}
		}

	}
	glog.Infof("successfully deallocate %v by container ID", deallocatedDeviceIDs)

	_, _, allocation, err := w.getAllocation(ctx, p)
	if err == nil {
		if dma.NeedP2PDMAConfigure(allocation.DeviceList) {
			if errs := dma.UnsetDevResourceFilePermissions(allocation.DeviceList); len(errs) > 0 {
				glog.Warningf("some device resource file may not be unset: %v", errs)
			}
		}
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		nodeState, index, _, err := w.getAllocation(ctx, p)
		if err != nil {
			return err
		}
		return w.removeAllocation(ctx, nodeState, index)
	})
	if err == nil {
		w.deleteMemo(p)
	}
	return err
}

func (w *PodWatcher) getAllocation(ctx context.Context,
	p corev1.Pod) (*spyrev1alpha1.SpyreNodeState, int, *spyrev1alpha1.Allocation, error) {
	podKey := getPodKey(p)
	nodeState, err := spyredevice.GetNodeStateForThisNode(ctx, w.spyreClient)
	if err != nil {
		return nil, -1, nil, err
	}
	for index, allocation := range nodeState.Status.AllocationList {
		if allocation.Pod.Name == p.Name && allocation.Pod.Namespace == p.Namespace {
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
	memo, found := w.getPodMemo(p)
	if !found {
		glog.Infof("%s/%s has been deleted, ignore %s event", p.Namespace, p.Name, event.EventType)
		return
	}
	err := w.allocate(ctx, p, memo.allocatedDeviceIDs, memo.resourceName)
	if err != nil {
		glog.Infof("failed to allocate devices from %s/%s: %v, requeue at %d attempt",
			p.Namespace, p.Name, err, event.Attempt)
		w.requeue(event)
	}
}

// allocate updates allocatedDeviceIDs in SpyreNodeState if not exists.
func (w *PodWatcher) allocate(ctx context.Context, p corev1.Pod, allocatedDeviceIDs []string, resourceName string) error {
	var err error
	var nodeState *spyrev1alpha1.SpyreNodeState

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if nodeState, err = spyredevice.GetNodeStateForThisNode(ctx, w.spyreClient); err != nil {
			return err
		}
		for _, allocation := range nodeState.Status.AllocationList {
			// skip if the item has already added
			if allocation.Pod.Name == p.Name && allocation.Pod.Namespace == p.Namespace {
				return nil
			}
		}
		// add new item if not exist
		newAllocation := spyrev1alpha1.Allocation{
			Pod: &spyrev1alpha1.Pod{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			DeviceList:   allocatedDeviceIDs,
			ResourcePool: resourceName,
		}
		nodeState.Status.AllocationList = append(nodeState.Status.AllocationList, newAllocation)
		nodeState.Status.Reservations = reconcileReservation(nodeState,
			&spyrev1alpha1.Pod{Name: p.Name, Namespace: p.Namespace}, allocatedDeviceIDs)
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
		glog.Infof("unexpected allocatedDeviceIDs length zero on pod %s/%s, skip AddAllocationToMemo", p.Namespace, p.Name)
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

func reconcileReservation(
	nodeState *spyrev1alpha1.SpyreNodeState, pod *spyrev1alpha1.Pod, allocatedDeviceIDs []string) map[string]spyrev1alpha1.Reservation {

	newReservations := make(map[string]spyrev1alpha1.Reservation)
	sAllocDevs := make([]string, len(allocatedDeviceIDs))
	copy(sAllocDevs, allocatedDeviceIDs)
	sort.Strings(sAllocDevs)

	for resName, reservation := range nodeState.Status.Reservations {
		for idx, p := range reservation.PodsUnderScheduling {
			if p.Name == pod.Name && p.Namespace == pod.Namespace {
				// remove
				reservation.PodsUnderScheduling[idx] = reservation.PodsUnderScheduling[len(reservation.PodsUnderScheduling)-1]
				reservation.PodsUnderScheduling = reservation.PodsUnderScheduling[:len(reservation.PodsUnderScheduling)-1]
			}
		}
		for idx, devs := range reservation.DeviceSets {
			sDevs := make([]string, len(devs))
			copy(sDevs, devs)
			sort.Strings(sDevs)
			if reflect.DeepEqual(sAllocDevs, sDevs) {
				// remove
				reservation.DeviceSets[idx] = reservation.DeviceSets[len(reservation.DeviceSets)-1]
				reservation.DeviceSets = reservation.DeviceSets[:len(reservation.DeviceSets)-1]
			}
		}
		if len(reservation.PodsUnderScheduling) > 0 || len(reservation.DeviceSets) > 0 {
			newReservations[resName] = reservation
		}
	}
	return newReservations
}
