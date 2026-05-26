/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package server_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/resources/server"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	spyrert "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/runtime"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	"github.com/ibm-aiu/spyre-operator/controllers/spyrepod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Pod Watcher", func() {
	Context("utility functions", func() {
		DescribeTable("RemoveAllocationIndex",
			func(index int, originalList []spyrev1alpha1.Allocation, expectedList []spyrev1alpha1.Allocation) {
				removedList := utils.RemoveAllocationIndex(originalList, index)
				Expect(len(removedList)).To(Equal(len(expectedList)))
				for i, allocation := range expectedList {
					Expect(allocation.Pod.Name).To(Equal(removedList[i].Pod.Name))
				}
			},
			Entry("remove one pod out of one", 0,
				[]spyrev1alpha1.Allocation{{Pod: &spyrev1alpha1.Pod{Name: "pod1"}}},
				[]spyrev1alpha1.Allocation{}),
			Entry("remove first pod out of two", 0,
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
				},
				[]spyrev1alpha1.Allocation{{Pod: &spyrev1alpha1.Pod{Name: "pod2"}}}),
			Entry("remove second pod out of two", 1,
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
				},
				[]spyrev1alpha1.Allocation{{Pod: &spyrev1alpha1.Pod{Name: "pod1"}}}),
			Entry("remove first pod out of three", 0,
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod3"}},
				},
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod3"}},
				}),
			Entry("remove middle pod out of three", 1,
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod3"}},
				},
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod3"}},
				}),
			Entry("remove last pod out of three", 2,
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod3"}},
				},
				[]spyrev1alpha1.Allocation{
					{Pod: &spyrev1alpha1.Pod{Name: "pod1"}},
					{Pod: &spyrev1alpha1.Pod{Name: "pod2"}},
				}),
		)

		DescribeTable("GetResourceNameFromPod",
			func(resourceNames []string, expectedResourceName string) {
				requests := make(corev1.ResourceList)
				for _, resourceName := range resourceNames {
					requests[corev1.ResourceName(resourceName)] = resource.MustParse("1")
				}
				p := &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{}, // dummy container 1
							{
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("1"),
									},
								}, // dummy container 2
							}, {
								Resources: corev1.ResourceRequirements{
									Requests: requests,
								},
							},
						},
					},
				}
				extractedResourceName := utils.GetResourceNameFromPod(p)
				Expect(extractedResourceName).To(Equal(expectedResourceName))
			},
			Entry("general pool", []string{"ibm.com/spyre_pf"}, "spyre_pf"),
			Entry("wrong prefix", []string{"some.org/spyre_pf"}, ""),
			Entry("specific pool", []string{"ibm.com/spyre_pf_0000_01"}, "spyre_pf_0000_01"),
			Entry("topology-aware pool", []string{"ibm.com/spyre_pf_tier0"}, "spyre_pf_tier0"),
			Entry("general pool with other resources", []string{"cpu", "ibm.com/spyre_pf"}, "spyre_pf"),
			Entry("wrong pool with other resources", []string{"cpu", "some.org/spyre_pf"}, ""),
			Entry("specific pool with other resources", []string{"cpu", "ibm.com/spyre_pf_0000_01"}, "spyre_pf_0000_01"),
			Entry("topology-aware pool with other resources", []string{"cpu", "ibm.com/spyre_pf_tier0"}, "spyre_pf_tier0"),
		)
	})

	Context("watcher process",
		func() {
			var nodeName = "watcher-test-node"
			var resourceName = "spyre_pf"
			var namespace = "default"
			var gracePeriod int64 = 0
			var succeededPhase = corev1.PodSucceeded
			var failedPhase = corev1.PodFailed

			var podWatcher *PodWatcher
			var allocatedCh chan types.AllocationInfo
			var mountedCh chan []string
			var deallocatedCh chan types.DeallocationInfo
			var stopCh chan interface{}
			var endSignal chan interface{}
			var allocatedMap map[string]bool
			var reservedPathWithTime map[string]time.Time
			var mu sync.RWMutex

			// dummy info server to handle allocate/deallocate/mount notifications
			var processChannelStart = func() {
				for {
					select {
					case <-stopCh:
						close(allocatedCh)
						close(deallocatedCh)
						close(mountedCh)
						close(endSignal)
						return
					case allocatedInfo, ok := <-allocatedCh:
						if ok {
							mu.Lock()
							for _, deviceId := range allocatedInfo.DeviceIDs {
								allocatedMap[deviceId] = true
							}
							mu.Unlock()
						}
					case deallocatedInfo, ok := <-deallocatedCh:
						if ok {
							mu.Lock()
							for _, deviceId := range deallocatedInfo.DeviceIDs {
								allocatedMap[deviceId] = false
							}
							mu.Unlock()
						}
					case mntHostPaths, ok := <-mountedCh:
						if ok {
							for _, mntPoint := range mntHostPaths {
								mu.Lock()
								_, ok := reservedPathWithTime[mntPoint]
								delete(reservedPathWithTime, mntPoint)
								mu.Unlock()
								Expect(ok).To(BeTrue())
							}
						}
					}
				}
			}

			BeforeEach(func() {
				ctx := context.Background()
				// create SpyreNodeState
				_ = os.Setenv("NODE_NAME", nodeName)
				s := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name:      nodeName,
						Namespace: namespace,
					},
					Spec: spyrev1alpha1.SpyreNodeStateSpec{
						NodeName: nodeName,
						SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
							{PciAddress: NodeDeviceIds[0], NumVfs: 1},
							{PciAddress: NodeDeviceIds[1], NumVfs: 1},
						},
					},
					Status: spyrev1alpha1.SpyreNodeStateStatus{},
				}
				_, err := SpyreClient.Create(ctx, s)
				Expect(err).To(BeNil())
				// set pod watcher
				allocatedCh = make(chan types.AllocationInfo, 1000)
				mountedCh = make(chan []string, 1000)
				deallocatedCh = make(chan types.DeallocationInfo, 1000)
				allocatedMap = make(map[string]bool)
				reservedPathWithTime = make(map[string]time.Time)
				stopCh = make(chan interface{})
				endSignal = make(chan interface{})
				go processChannelStart()
				for _, deviceID := range NodeDeviceIds {
					allocatedMap[deviceID] = false
				}
				podWatcher, err = NewPodWatcher(Cfg, allocatedCh, mountedCh, deallocatedCh)
				Expect(err).To(BeNil())
				podWatcher.NotifyInitialAllocationList()
				podWatcher.Start()
			})

			AfterEach(func() {
				ctx := context.Background()
				// delete SpyreNodeState
				err := SpyreClient.Delete(ctx, nodeName, &client.DeleteOptions{})
				Expect(err).To(Succeed())
				podWatcher.Stop()
				close(stopCh)
				<-endSignal
			})

			// define common process functions
			// createPodProcess creates pod resource
			var createPodProcess = func(testName string, deviceNum string) {
				defer GinkgoRecover()
				By("creating pod")
				p := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: namespace,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  testName,
								Image: "fakeImage",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										"ibm.com/spyre_pf": resource.MustParse(deviceNum),
									},
									Limits: corev1.ResourceList{
										"ibm.com/spyre_pf": resource.MustParse(deviceNum),
									},
								},
							},
						},
						NodeName:      utils.GetNodeName(),
						RestartPolicy: corev1.RestartPolicyNever,
					},
				}
				Expect(spyrepod.IsSpyrePod(p)).To(BeTrue())
				_, err := K8sClientset.CoreV1().Pods(namespace).Create(context.Background(), p, metav1.CreateOptions{})
				Expect(err).To(BeNil())
			}
			// allocateProcess adds new container to mock-up runtime and append in allocatedCh
			var allocateProcess = func(testName string, deviceIds []string) (string, string, string, string) {
				defer GinkgoRecover()
				By("allocating devices to pod")
				// new container from mock-up runtime
				testHostPath, containerConfigHostPath, containerMetricsHostPath, containerId := CreateContainer(testName, deviceIds)
				// fake result from calling Allocate
				allocatedCh <- types.AllocationInfo{
					DeviceIDs:    deviceIds,
					MountPoints:  []string{containerConfigHostPath, containerMetricsHostPath},
					ResourceName: resourceName,
				}
				mu.Lock()
				reservedPathWithTime[containerConfigHostPath] = time.Now()
				reservedPathWithTime[containerMetricsHostPath] = time.Now()
				mu.Unlock()
				Eventually(func(g Gomega) {
					// allocateCh is processed (by allocateProcess)
					for _, deviceId := range deviceIds {
						mu.Lock()
						allocated, found := allocatedMap[deviceId]
						mu.Unlock()
						g.Expect(found).To(BeTrue())
						g.Expect(allocated).To(BeTrue())
					}
				}).WithTimeout(10 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
				return testHostPath, containerConfigHostPath, containerMetricsHostPath, containerId
			}
			// updatePodProcess updates pod's status.
			var updatePodProcess = func(
				testName, containerId, containerConfigHostPath, containerMetricsHostPath string,
				deviceIds []string, phase corev1.PodPhase, wg *sync.WaitGroup,
			) {
				defer GinkgoRecover()
				defer wg.Done()
				ctx := context.Background()
				By("updating pod status")
				// the container in runtime must exist (require allocateProcess call).
				_, err := spyrert.GetMountHostPath(containerId)
				Expect(err).To(BeNil())
				p, err := K8sClientset.CoreV1().Pods(namespace).Get(context.Background(), testName, metav1.GetOptions{})
				Expect(err).To(BeNil())
				// must have no container status before
				Expect(len(p.Status.ContainerStatuses)).To(Equal(0))
				p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{
					ContainerID: containerId,
				})
				p.Status.Phase = phase
				_, err = K8sClientset.CoreV1().Pods(namespace).UpdateStatus(context.Background(), p, metav1.UpdateOptions{})
				Expect(err).To(BeNil())
				allocatedDeviceIDs, mntHostPaths, _ := spyrert.GetDevicesAndMounts(p)
				Expect(allocatedDeviceIDs).To(Equal(deviceIds))
				Expect(containerConfigHostPath).To(BeElementOf(mntHostPaths))
				Expect(containerMetricsHostPath).To(BeElementOf(mntHostPaths))
				By("waiting until pod update is processed")
				Eventually(func(g Gomega) {
					// node state is updated
					nodeState, err := spyredevice.GetNodeStateForThisNode(ctx, SpyreClient)
					g.Expect(err).To(BeNil())
					allocations := nodeState.Status.AllocationList
					var exist bool
					for _, allocation := range allocations {
						if allocation.Pod.Name == testName {
							g.Expect(allocation.Pod.Namespace).To(Equal(namespace))
							g.Expect(allocation.DeviceList).To(Equal(deviceIds))
							g.Expect(allocation.ResourcePool).To(Equal("spyre_pf"))
							exist = true
						}
					}
					g.Expect(exist).To(BeTrue())
					// mounted path is claimed
					mu.Lock()
					_, found := reservedPathWithTime[containerConfigHostPath]
					Expect(found).To(BeFalse())
					_, found = reservedPathWithTime[containerMetricsHostPath]
					Expect(found).To(BeFalse())
					mu.Unlock()
				}).WithTimeout(10 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
			}
			// checkDeviceReleased checks whether devices are released.
			var checkDeviceReleased = func(containerConfigHostPath, containerMetricsHostPath string, deviceIds []string) {
				defer GinkgoRecover()
				ctx := context.Background()
				By("checking devices are released")
				Eventually(func(g Gomega) {
					nodeState, err := spyredevice.GetNodeStateForThisNode(ctx, SpyreClient)
					g.Expect(err).To(BeNil())
					// node state is deleted
					g.Expect(nodeState.Status.AllocationList).To(BeEmpty())
					// container path must be deleted
					_, err = os.Stat(containerConfigHostPath)
					g.Expect(os.IsNotExist(err)).To(BeTrue())
					_, err = os.Stat(containerMetricsHostPath)
					g.Expect(os.IsNotExist(err)).To(BeTrue())
					// deallocateCh is processed
					for _, deviceId := range deviceIds {
						mu.Lock()
						allocated, found := allocatedMap[deviceId]
						mu.Unlock()
						g.Expect(found).To(BeTrue())
						g.Expect(allocated).To(BeFalse())
					}
				}).WithTimeout(10 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
			}
			// deletePodProcess deletes a pod and waits until pod removed.
			var deletePodProcess = func(testName, containerId string, wg *sync.WaitGroup) {
				defer GinkgoRecover()
				if wg != nil {
					defer wg.Done()
				}
				By("deleting pod")
				err := K8sClientset.CoreV1().Pods(namespace).Delete(
					context.Background(), testName,
					metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
				Expect(err).To(BeNil())
				mu.Lock()
				DeleteContainer(containerId)
				mu.Unlock()
				By("waiting until pod is deleted")
				Eventually(func(g Gomega) {
					// pod is deleted
					_, err := K8sClientset.CoreV1().Pods(namespace).Get(context.Background(), testName, metav1.GetOptions{})
					g.Expect(err).NotTo(BeNil())
					g.Expect(errors.IsNotFound(err)).To(BeTrue())
				}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
			}

			DescribeTable("allocate/deallocate", Serial,
				func(testName string, deviceIdsList [][]string, podPhase corev1.PodPhase, releasePhase *corev1.PodPhase) {
					var wg sync.WaitGroup
					var (
						testNames                 = make([]string, len(deviceIdsList))
						testHostPaths             = make([]string, len(deviceIdsList))
						containerConfigHostPaths  = make([]string, len(deviceIdsList))
						containerMetricsHostPaths = make([]string, len(deviceIdsList))
						containerIds              = make([]string, len(deviceIdsList))
					)

					for i := range deviceIdsList {
						testNames[i] = fmt.Sprintf("%s-%d", testName, i)
						createPodProcess(testNames[i], fmt.Sprintf("%d", len(deviceIdsList[i])))
						testHostPaths[i], containerConfigHostPaths[i], containerMetricsHostPaths[i], containerIds[i] =
							allocateProcess(testNames[i], deviceIdsList[i])
						defer func(path string) { _ = os.RemoveAll(path) }(testHostPaths[i])
						wg.Add(1)
						go updatePodProcess(testNames[i], containerIds[i],
							containerConfigHostPaths[i], containerMetricsHostPaths[i],
							deviceIdsList[i], podPhase, &wg)
					}
					wg.Wait()
					for i := range deviceIdsList {
						if releasePhase == nil {
							wg.Add(1)
							go deletePodProcess(testNames[i], containerIds[i], &wg)
						} else {
							wg.Add(1)
							go func(j int) {
								defer wg.Done()
								By(fmt.Sprintf("updating pod's phase to %s", *releasePhase))
								p, err := K8sClientset.CoreV1().Pods(namespace).Get(context.Background(), testNames[j], metav1.GetOptions{})
								Expect(err).To(BeNil())
								p.Status.Phase = *releasePhase
								_, err = K8sClientset.CoreV1().Pods(namespace).UpdateStatus(context.Background(), p, metav1.UpdateOptions{})
								Expect(err).To(BeNil())
							}(i)
						}
					}
					wg.Wait()
					for i, deviceIds := range deviceIdsList {
						checkDeviceReleased(containerConfigHostPaths[i], containerMetricsHostPaths[i], deviceIds)
						if releasePhase != nil {
							deletePodProcess(testNames[i], containerIds[i], nil)
						}
					}
				},
				Entry("single device pending", "single-spyre-pending",
					[][]string{{NodeDeviceIds[0]}, {NodeDeviceIds[1]}}, corev1.PodPending, nil),
				Entry("single device running", "single-spyre-running",
					[][]string{{NodeDeviceIds[0]}, {NodeDeviceIds[1]}}, corev1.PodRunning, nil),
				Entry("single device succeeded", "single-spyre-succeeded",
					[][]string{{NodeDeviceIds[0]}, {NodeDeviceIds[1]}}, corev1.PodRunning, &succeededPhase),
				Entry("single device failed", "single-spyre-failed",
					[][]string{{NodeDeviceIds[0]}, {NodeDeviceIds[1]}}, corev1.PodRunning, &failedPhase),
				Entry("multiple devices", "multi-spyre",
					[][]string{NodeDeviceIds}, corev1.PodRunning, nil),
			)

			DescribeTable("RemoveReservation",
				func(
					allocatedDeviceIDs []string,
					pod *spyrev1alpha1.Pod,
					orig map[string]spyrev1alpha1.Reservation,
					expected map[string]spyrev1alpha1.Reservation,
				) {
					nodeState := &spyrev1alpha1.SpyreNodeState{Status: spyrev1alpha1.SpyreNodeStateStatus{Reservations: orig}}
					r := ReconcileReservation(nodeState, pod, allocatedDeviceIDs)
					Expect(r).Should(Equal(expected))
				}, Entry("remove one of reservations (order insensitive)",
					[]string{"d1", "d2"},
					&spyrev1alpha1.Pod{Name: "p1", Namespace: "ns1"},
					map[string]spyrev1alpha1.Reservation{"spyre_pf": {
						PodsUnderScheduling: []spyrev1alpha1.Pod{
							{Name: "p1", Namespace: "ns1"},
							{Name: "p2", Namespace: "ns1"}},
						DeviceSets: [][]string{{"d2", "d1"}, {"d3", "d4"}}}},
					map[string]spyrev1alpha1.Reservation{"spyre_pf": {
						PodsUnderScheduling: []spyrev1alpha1.Pod{{Name: "p2", Namespace: "ns1"}},
						DeviceSets:          [][]string{{"d3", "d4"}}}}),
				Entry("remove all of reservations",
					[]string{"d3", "d4"},
					&spyrev1alpha1.Pod{Name: "p2", Namespace: "ns1"},
					map[string]spyrev1alpha1.Reservation{"spyre_pf": {
						PodsUnderScheduling: []spyrev1alpha1.Pod{{Name: "p2", Namespace: "ns1"}},
						DeviceSets:          [][]string{{"d3", "d4"}}}},
					map[string]spyrev1alpha1.Reservation{},
				))

			It("must error out on getting allocation of non-existing Pod", func() {
				p := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "dummy",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "dummy",
								Image: "fakeImage",
							},
						},
						NodeName:      utils.GetNodeName(),
						RestartPolicy: corev1.RestartPolicyNever,
					},
				}
				ctx := context.Background()
				_, _, _, err := podWatcher.GetAllocation(ctx, p)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot find allocation"))
			})
		})

})
