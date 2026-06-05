/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice_test

import (
	"context"
	goerrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/factory"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	"github.com/jaypipes/ghw/pkg/pci"
	"github.com/jaypipes/pcidb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	testNode1 = "node1"
	testNode2 = "node2"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

// var scheme = runtime.NewScheme()
func createNewNamespace(ctx context.Context) string {
	namespace := "ns" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	ns := &corev1.Namespace{}
	ns.Name = namespace
	err := k8sClient.Create(ctx, ns)
	Expect(err).To(BeNil())
	return namespace
}

var _ = Describe("Spyre State Updater", func() {

	_ = os.Setenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
	ctx := context.Background()

	Context("Custom kube client for SpyreNodeState and SpyreClusterPolicy", func() {
		var spyreClient *spyreclient.SpyreClient
		var err error
		nodeList := []string{"node1", "node2"}

		Context("general CRUD functions", func() {

			BeforeEach(func() {
				spyreClient, err = spyreclient.NewClient(context.Background(), cfg)
				Expect(err).To(BeNil())
				Expect(spyreClient).NotTo((BeNil()))
				for _, node := range nodeList {
					s := &spyrev1alpha1.SpyreNodeState{
						ObjectMeta: metav1.ObjectMeta{
							Name:      node,
							Namespace: metav1.NamespaceAll,
						},
						Spec: spyrev1alpha1.SpyreNodeStateSpec{
							NodeName: node,
							SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
								{PciAddress: "0000:01:00.0", NumVfs: 0},
							},
						},
						Status: spyrev1alpha1.SpyreNodeStateStatus{},
					}
					_, err = spyreClient.Create(ctx, s)
					Expect(err).To(BeNil())
				}
				nsList, err := spyreClient.List(ctx, &client.ListOptions{})
				Expect(err).To(BeNil())
				Expect(len(nsList.Items)).Should(Equal(len(nodeList)))
			})
			AfterEach(func() {
				err = spyreClient.DeleteAll(ctx)
				Expect(err).To(BeNil())
			})

			It("can create/read corev1 resources through a new kube client", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				ns := createNewNamespace(ctx)
				p := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: ns},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "c1", Image: "image"}},
					},
				}
				err = k8sClient.Create(ctx, p)
				Expect(err).To(BeNil())
				p2 := &corev1.Pod{}
				err = k8sClient.Get(ctx, client.ObjectKey{Name: "p1", Namespace: ns}, p2)
				Expect(err).To(BeNil())
				Expect(p2.Name).Should(Equal("p1"))
			})

			It("can create/get/delete a new SpyreNodeState resource", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeState := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testnodestate",
						Namespace: metav1.NamespaceAll,
					},
				}
				By("creating new SpyreNodeState")
				nodeState, err = spyreClient.Create(ctx, nodeState)
				Expect(err).NotTo(HaveOccurred())
				Expect(nodeState.Name).Should(Equal("testnodestate"))
				By("getting new SpyreNodeState")
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(nodeState.Name).Should(Equal("testnodestate"))
				By("deleting new SpyreNodeState")
				err = spyreClient.Delete(ctx, "testnodestate", &client.DeleteOptions{})
				Expect(err).To(BeNil())
				_, err = spyreClient.Get(ctx, "testnodestate")
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})

			It("can list SpyreNodeState resources", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeStateList, err := spyreClient.List(ctx, &client.ListOptions{})
				Expect(err).To(BeNil())
				Expect(len(nodeStateList.Items)).Should(Equal(2))
				Expect(nodeStateList.Items[0].Name).Should(BeElementOf(nodeList))
				Expect(nodeStateList.Items[1].Name).Should(BeElementOf(nodeList))
			})

			It("can prevent updating out-of-sync SpyreNodeState resource", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				ns1 := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testnodestate",
						Namespace: metav1.NamespaceAll,
					},
				}
				_, err = spyreClient.Create(ctx, ns1)
				Expect(err).To(BeNil())
				ns1, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(ns1.Name).Should(Equal("testnodestate"))
				nName2 := "newNodeName"
				ns1.Spec.NodeName = nName2
				ns2, err := spyreClient.Update(ctx, ns1, false)
				Expect(err).To(BeNil())
				Expect(ns2.Spec.NodeName).Should(Equal(nName2))
				nName3 := "furtherNewNodeName"
				ns1.Spec.NodeName = nName3
				_, err = spyreClient.Update(ctx, ns1, false) // this must be fail
				var statusErr *errors.StatusError
				if goerrors.As(err, &statusErr) {
					Expect(statusErr.ErrStatus.Reason).Should(Equal(metav1.StatusReasonConflict))
				}
				By("deleting testnodestate")
				err = spyreClient.Delete(ctx, "testnodestate", &client.DeleteOptions{})
				Expect(err).To(BeNil())
				_, err = spyreClient.Get(ctx, "testnodestate")
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})

			It("can check conflict error", func() {
				err := fmt.Errorf("my error")
				Expect(spyredevice.IsConflictError(err)).Should(BeFalse())
				err = &errors.StatusError{
					ErrStatus: metav1.Status{
						Reason: metav1.StatusReasonConflict,
					},
				}
				Expect(spyredevice.IsConflictError(err)).Should(BeTrue())
			})

			It("can add a device to the spec of a SpyreNodeState resources", func() {
				err := os.Setenv(spyredevice.NodeNameEnvKey, "node2")
				Expect(err).NotTo(HaveOccurred())
				device := spyredevice.GeneratePseudoDevice("0000:99:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				devices := []types.PciDevice{
					pciDevice,
				}
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				Expect(nodeState.Spec.NodeName).Should(Equal("node2"))
				nsList, err := spyreClient.List(ctx, &client.ListOptions{})
				Expect(err).To(BeNil())
				for _, nodeState := range nsList.Items {
					switch nodeState.Spec.NodeName {
					case testNode1:
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
					case testNode2:
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(2))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
						Expect(nodeState.Spec.SpyreInterfaces[1].PciAddress).Should(Equal("0000:99:00.0"))
					}
				}
			})

			It("can add a vf device to the spec of SpyreNodeState resources", func() {
				err := os.Setenv(spyredevice.NodeNameEnvKey, testNode1)
				Expect(err).NotTo(HaveOccurred())
				deviceVf0 := spyredevice.GeneratePseudoDevice("0000:01:00.1", resources.VfProductId)
				deviceVf1 := spyredevice.GeneratePseudoDevice("0000:01:00.2", resources.VfProductId)
				pciDeviceVf0 := spyredevice.NewPseudoPciDevice(deviceVf0)
				pciDeviceVf1 := spyredevice.NewPseudoPciDevice(deviceVf1)
				devices := []types.PciDevice{
					pciDeviceVf0, pciDeviceVf1,
				}
				_, err = spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				nodeState, err := spyreClient.Get(ctx, testNode1)
				Expect(err).To(BeNil())
				Expect(nodeState.Spec.NodeName).Should(Equal(testNode1))
				Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1))
				Expect(nodeState.Spec.SpyreInterfaces[0].NumVfs).Should(Equal(2))
				Expect(len(nodeState.Spec.SpyreInterfaces[0].Vfs)).Should(Equal(2))
				for _, vf := range nodeState.Spec.SpyreInterfaces[0].Vfs {
					Expect(vf).To(BeElementOf("0000:01:00.1", "0000:01:00.2"))
				}
			})

			It("ignores device addition request if it has already been in the spec of a SpyreNodeState resources", func() {
				device := spyredevice.GeneratePseudoDevice("0000:01:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				devices := []types.PciDevice{
					pciDevice,
				}
				_ = os.Setenv(spyredevice.NodeNameEnvKey, "node2")
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				Expect(nodeState.Spec.NodeName).Should(Equal("node2"))
				nsList, err := spyreClient.List(ctx, &client.ListOptions{})
				Expect(err).To(BeNil())
				for _, nodeState := range nsList.Items {
					switch nodeState.Spec.NodeName {
					case testNode1:
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
					case testNode2:
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1),
							fmt.Sprintf("interfaces: %v", nodeState.Spec.SpyreInterfaces))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
					}
				}
			})

			It("can get SpyreClusterPolicy", func() {
				By("deploying SpyreClusterPolicy")
				scp := spyrev1alpha1.SpyreClusterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "scp",
					},
					Spec: spyrev1alpha1.SpyreClusterPolicySpec{
						ExperimentalMode: []spyrev1alpha1.SpyreClusterPolicyExperimentalMode{spyrev1alpha1.PerDeviceAllocationMode},
					},
				}
				_, err = spyreClient.CreateSpyreClusterPolicy(ctx, &scp)
				Expect(err).To(BeNil())
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(err).To(BeNil())
				result, err := spyreClient.GetSpyreClusterPolicy(ctx, "scp")
				Expect(err).To(BeNil())
				Expect(result.Name).Should(Equal(scp.Name))
				Expect(result.Spec.ExperimentalMode).Should(ContainElement(spyrev1alpha1.PerDeviceAllocationMode))
			})

			pf99 := spyredevice.GeneratePseudoDevice("0000:99:00.0", resources.PfProductId)
			vf99 := spyredevice.GeneratePseudoDevice("0000:99:00.1", resources.VfProductId)
			vf99_2 := spyredevice.GeneratePseudoDevice("0000:99:00.2", resources.VfProductId)
			pf99pciDevice := spyredevice.NewPseudoPciDevice(pf99)
			vf99pciDevice := spyredevice.NewPseudoPciDevice(vf99)
			vf99pciDevice_2 := spyredevice.NewPseudoPciDevice(vf99_2)
			singleExpectedVfs := []string{"0000:99:00.1"}
			twoExpectedVfs := []string{"0000:99:00.1", "0000:99:00.2"}
			pf01 := spyredevice.GeneratePseudoDevice("0000:01:00.0", resources.PfProductId)
			vf01 := spyredevice.GeneratePseudoDevice("0000:01:00.1", resources.VfProductId)
			pf01pciDevice := spyredevice.NewPseudoPciDevice(pf01)
			vf01pciDevice := spyredevice.NewPseudoPciDevice(vf01)
			node2ValidCheck := func(nodeState *spyrev1alpha1.SpyreNodeState, expectedVfs []string) {
				Expect(nodeState.Spec.NodeName).Should(Equal("node2"))
				nsList, err := spyreClient.List(ctx, &client.ListOptions{})
				Expect(err).To(BeNil())
				for _, nodeState := range nsList.Items {
					switch nodeState.Spec.NodeName {
					case testNode1:
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
						Expect(nodeState.Spec.SpyreInterfaces[0].NumVfs).Should(Equal(0))
					case "node2":
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(2))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
						Expect(nodeState.Spec.SpyreInterfaces[0].NumVfs).Should(Equal(0))
						Expect(nodeState.Spec.SpyreInterfaces[1].PciAddress).Should(Equal("0000:99:00.0"))
						Expect(nodeState.Spec.SpyreInterfaces[1].NumVfs).Should(Equal(len(expectedVfs)))
						for _, vf := range nodeState.Spec.SpyreInterfaces[1].Vfs {
							Expect(vf).Should(BeElementOf(expectedVfs))
						}
					}
				}
			}
			It("can add vf to correct pf", func() {
				devices := []types.PciDevice{
					pf99pciDevice, vf99pciDevice,
				}
				_ = os.Setenv(spyredevice.NodeNameEnvKey, "node2")
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				node2ValidCheck(nodeState, singleExpectedVfs)

			})
			It("can add with vf", func() {
				devices := []types.PciDevice{
					vf99pciDevice,
				}
				_ = os.Setenv(spyredevice.NodeNameEnvKey, "node2")
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				node2ValidCheck(nodeState, singleExpectedVfs)
			})
			It("can process vf in a swop order", func() {
				devices := []types.PciDevice{
					vf01pciDevice, pf01pciDevice,
				}
				_ = os.Setenv(spyredevice.NodeNameEnvKey, "node2")
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				Expect(nodeState.Spec.NodeName).Should(Equal("node2"))
				nsList, err := spyreClient.List(ctx, &client.ListOptions{})
				Expect(err).To(BeNil())
				for _, nodeState := range nsList.Items {
					switch nodeState.Spec.NodeName {
					case "node1":
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
						Expect(nodeState.Spec.SpyreInterfaces[0].NumVfs).Should(Equal(0))
					case "node2":
						Expect(len(nodeState.Spec.SpyreInterfaces)).Should(Equal(1))
						Expect(nodeState.Spec.SpyreInterfaces[0].PciAddress).Should(Equal("0000:01:00.0"))
						Expect(nodeState.Spec.SpyreInterfaces[0].NumVfs).Should(Equal(1))
						Expect(nodeState.Spec.SpyreInterfaces[0].Vfs[0]).Should(Equal("0000:01:00.1"))
					}
				}
			})
			It("can process more than one vf", func() {
				devices := []types.PciDevice{
					vf99pciDevice, vf99pciDevice_2,
				}
				_ = os.Setenv(spyredevice.NodeNameEnvKey, "node2")
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				node2ValidCheck(nodeState, twoExpectedVfs)
			})
		})

		Context("spec handling", func() {
			ctx := context.Background()

			AfterEach(func() {
				err = spyreClient.DeleteAll(ctx)
				Expect(err).To(BeNil())
			})

			It("can update the spec of a SpyreNodeState resource", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeState := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnodestate",
					},
				}
				_, err = spyreClient.Create(ctx, nodeState)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(nodeState.Name).Should(Equal("testnodestate"))
				newNodeName := "newNodeName"
				nodeState.Spec.NodeName = newNodeName
				nodeState, err = spyreClient.Update(ctx, nodeState, false)
				Expect(err).To(BeNil())
				Expect(nodeState.Spec.NodeName).Should(Equal(newNodeName))
				By("deleting testnodestate")
				err = spyreClient.Delete(ctx, "testnodestate", &client.DeleteOptions{})
				Expect(err).To(BeNil())
				_, err = spyreClient.Get(ctx, "testnodestate")
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})
			It("can add interface to an SpyreNodeState which already contains interface", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeState := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name: "s1",
					},
					Spec: spyrev1alpha1.SpyreNodeStateSpec{
						NodeName: "s1",
						SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
							{PciAddress: "00:11", NumVfs: 1},
						},
					},
				}
				_, err = spyreClient.Create(ctx, nodeState)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "s1")
				Expect(err).To(BeNil())
				Expect(nodeState.Name).Should(Equal("s1"))
				nodeState.Status.AllocationList = []spyrev1alpha1.Allocation{
					{DeviceList: []string{"0000:99:00.0"}},
				}
				_, err = spyreClient.UpdateStatus(ctx, nodeState, false)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "s1")
				Expect(err).To(BeNil())
				Expect(nodeState.Status.AllocationList[0].DeviceList).Should(Equal([]string{"0000:99:00.0"}))
			})
			It("can update the status of a SpyreNodeState resource", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeState := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnodestate",
					},
				}
				_, err = spyreClient.Create(ctx, nodeState)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(nodeState.Name).Should(Equal("testnodestate"))
				nodeState.Status.AllocationList = []spyrev1alpha1.Allocation{
					{DeviceList: []string{"0000:99:00.0"}},
				}
				_, err = spyreClient.UpdateStatus(ctx, nodeState, false)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(nodeState.Status.AllocationList[0].DeviceList).Should(Equal([]string{"0000:99:00.0"}))
			})
			It("can update the reservation status twice for same resource", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeState := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnodestate",
					},
				}
				_, err = spyreClient.Create(ctx, nodeState)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(nodeState.Name).Should(Equal("testnodestate"))

				// add first one
				nodeState.Status.Reservations = map[string]spyrev1alpha1.Reservation{
					"spyre_pf": {DeviceSets: [][]string{{"0000:99:00.0"}}},
				}
				_, err = spyreClient.UpdateStatus(ctx, nodeState, false)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				ds := nodeState.Status.Reservations["spyre_pf"].DeviceSets
				Expect(ds[0]).Should(Equal([]string{"0000:99:00.0"}))

				// add second one
				r := nodeState.Status.Reservations["spyre_pf"]
				r.DeviceSets = append(r.DeviceSets, []string{"0000:0a:00.0"})
				nodeState.Status.Reservations["spyre_pf"] = r
				_, err = spyreClient.UpdateStatus(ctx, nodeState, false)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				ds = nodeState.Status.Reservations["spyre_pf"].DeviceSets
				Expect(ds[0]).Should(Equal([]string{"0000:99:00.0"}))
				Expect(ds[1]).Should(Equal([]string{"0000:0a:00.0"}))
			})

			It("can update the reservation status for two resources", func() {
				spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
				Expect(spyreClient).NotTo(BeNil())
				Expect(err).To(BeNil())
				nodeState := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testnodestate",
					},
				}
				_, err = spyreClient.Create(ctx, nodeState)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				Expect(nodeState.Name).Should(Equal("testnodestate"))

				// add first one
				nodeState.Status.Reservations = map[string]spyrev1alpha1.Reservation{
					"spyre_pf": {DeviceSets: [][]string{{"0000:99:00.0"}}},
				}
				_, err = spyreClient.UpdateStatus(ctx, nodeState, false)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				ds := nodeState.Status.Reservations["spyre_pf"].DeviceSets
				Expect(ds[0]).Should(Equal([]string{"0000:99:00.0"}))

				// add second one
				nodeState.Status.Reservations["spyre_pf_nearest"] = spyrev1alpha1.Reservation{DeviceSets: [][]string{{"00:aa"}}}
				_, err = spyreClient.UpdateStatus(ctx, nodeState, false)
				Expect(err).To(BeNil())
				nodeState, err = spyreClient.Get(ctx, "testnodestate")
				Expect(err).To(BeNil())
				ds = nodeState.Status.Reservations["spyre_pf"].DeviceSets
				Expect(ds[0]).Should(Equal([]string{"0000:99:00.0"}))
				ds = nodeState.Status.Reservations["spyre_pf_nearest"].DeviceSets
				Expect(ds[0]).Should(Equal([]string{"00:aa"}))
				By("deleting testnodestate")
				err = spyreClient.Delete(ctx, "testnodestate", &client.DeleteOptions{})
				Expect(err).To(BeNil())
				_, err = spyreClient.Get(ctx, "testnodestate")
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("status handling", func() {
			ctx := context.Background()

			BeforeEach(func() {
				spyreClient, err = spyreclient.NewClient(context.Background(), cfg)
				Expect(err).To(BeNil())
				Expect(spyreClient).NotTo(BeNil())

				// Create test node states
				for _, node := range nodeList {
					s := &spyrev1alpha1.SpyreNodeState{
						ObjectMeta: metav1.ObjectMeta{
							Name:      node,
							Namespace: metav1.NamespaceAll,
						},
						Spec: spyrev1alpha1.SpyreNodeStateSpec{
							NodeName: node,
						},
					}
					_, err = spyreClient.Create(ctx, s)
					Expect(err).To(BeNil())
				}
			})

			AfterEach(func() {
				err = spyreClient.DeleteAll(ctx)
				Expect(err).To(BeNil())
			})

			It("no detected device", func() {
				devices := []types.PciDevice{}
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, true, nil)
				Expect(err).To(BeNil())
				Expect(nodeState.Status.Conditions).To(HaveLen(1))
				Expect(nodeState.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionUnknown))
				Expect(nodeState.Status.Conditions[0].Reason).To(BeEquivalentTo(spyredevice.ReasonNoDetectedDevice))
				Expect(nodeState.Status.UnhealthyDevices).To(HaveLen(0))
			})

			It("all healthy", func() {
				device := spyredevice.GeneratePseudoDevice("0000:99:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				devices := []types.PciDevice{
					pciDevice,
				}
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, true, nil)
				Expect(err).To(BeNil())
				Expect(nodeState.Status.Conditions).To(HaveLen(1))
				Expect(nodeState.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionTrue))
				Expect(nodeState.Status.Conditions[0].Reason).To(BeEquivalentTo(spyredevice.ReasonAllDevicesHealthy))
				Expect(nodeState.Status.UnhealthyDevices).To(HaveLen(0))
			})

			It("unhealthy without report", func() {
				device := spyredevice.GeneratePseudoDevice("0000:99:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				pciDevice.SetHealth(pluginapi.Unhealthy)
				devices := []types.PciDevice{
					pciDevice,
				}
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, true, nil)
				Expect(err).To(BeNil())
				Expect(nodeState.Status.Conditions).To(HaveLen(1))
				Expect(nodeState.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionFalse))
				Expect(nodeState.Status.Conditions[0].Reason).To(BeEquivalentTo(spyredevice.ReasonSomeDeviceUnhealthy))
				Expect(nodeState.Status.UnhealthyDevices).To(HaveLen(1))
				Expect(nodeState.Status.UnhealthyDevices[0].ID).To(Equal("0000:99:00.0"))
				Expect(nodeState.Status.UnhealthyDevices[0].State).
					To(BeEquivalentTo(pb.DEVICE_STATE_DEVICE_STATE_UNSPECIFIED.String()))
			})

			It("unhealthy with report", func() {
				errState := pb.DEVICE_STATE_IN_ERROR.String()
				device := spyredevice.GeneratePseudoDevice("0000:99:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				pciDevice.SetHealth(pluginapi.Unhealthy)
				devices := []types.PciDevice{
					pciDevice,
				}
				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, true,
					[]spyrev1alpha1.UnhealthyDevice{{ID: "0000:99:00.0", State: errState}})
				Expect(err).To(BeNil())
				Expect(nodeState.Status.Conditions).To(HaveLen(1))
				Expect(nodeState.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionFalse))
				Expect(nodeState.Status.Conditions[0].Reason).To(BeEquivalentTo(spyredevice.ReasonSomeDeviceUnhealthy))
				Expect(nodeState.Status.UnhealthyDevices).To(HaveLen(1))
				Expect(nodeState.Status.UnhealthyDevices[0].ID).To(Equal("0000:99:00.0"))
				Expect(nodeState.Status.UnhealthyDevices[0].State).
					To(BeEquivalentTo(errState))
			})
		})

		Context("topology integration in WriteSpyreInterfacesToNodeState", func() {
			var originalPciTopology *pcitopov2.Pcitopo

			BeforeEach(func() {
				spyreClient, err = spyreclient.NewClient(context.Background(), cfg)
				Expect(err).To(BeNil())
				Expect(spyreClient).NotTo(BeNil())

				// Save original PciTopology
				originalPciTopology = spyretopo.PciTopology

				// Load test topology file and set it globally
				testDataDir := filepath.Join("..", "..", "test", "data")
				topoFile := filepath.Join(testDataDir, "topo_v2.json")
				topoData, err := os.ReadFile(topoFile)
				Expect(err).To(BeNil())

				testTopo, err := pcitopov2.UnmarshalPciTopo(topoData)
				Expect(err).To(BeNil())
				spyretopo.PciTopology = &testTopo

				// Create test node states
				for _, node := range nodeList {
					s := &spyrev1alpha1.SpyreNodeState{
						ObjectMeta: metav1.ObjectMeta{
							Name:      node,
							Namespace: metav1.NamespaceAll,
						},
						Spec: spyrev1alpha1.SpyreNodeStateSpec{
							NodeName: node,
						},
					}
					_, err = spyreClient.Create(ctx, s)
					Expect(err).To(BeNil())
				}
			})

			AfterEach(func() {
				err = spyreClient.DeleteAll(ctx)
				Expect(err).To(BeNil())

				// Restore original PciTopology
				spyretopo.PciTopology = originalPciTopology
			})

			It("should update topology in pseudo mode without health filtering", func() {
				_ = os.Setenv(spyredevice.NodeNameEnvKey, "node1")
				_ = os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), spyreconst.ModeEnabledValue)

				device := spyredevice.GeneratePseudoDevice("0000:29:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				devices := []types.PciDevice{pciDevice}

				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				Expect(nodeState).NotTo(BeNil())

				// Verify topology was updated
				Expect(nodeState.Spec.Pcitopo).NotTo(BeEmpty())

				err = os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
				Expect(err).NotTo(HaveOccurred())
			})

			It("should apply health filtering in non-pseudo mode", func() {
				err := os.Setenv(spyredevice.NodeNameEnvKey, "node1")
				Expect(err).NotTo(HaveOccurred())
				err = os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
				Expect(err).NotTo(HaveOccurred())

				device1 := spyredevice.GeneratePseudoDevice("0000:29:00.0", resources.PfProductId)
				pciDevice1 := spyredevice.NewPseudoPciDevice(device1)

				device2 := spyredevice.GeneratePseudoDevice("0000:a9:00.0", resources.PfProductId)
				pciDevice2 := spyredevice.NewPseudoPciDevice(device2)

				devices := []types.PciDevice{pciDevice1, pciDevice2}

				nodeState, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())
				Expect(nodeState).NotTo(BeNil())

				// Verify topology was updated with health filtering applied
				Expect(nodeState.Spec.Pcitopo).NotTo(BeEmpty())
			})

			It("should clear topology when GetPciTopology fails and Pcitopo exists", func() {
				err := os.Setenv(spyredevice.NodeNameEnvKey, "node1")
				Expect(err).NotTo(HaveOccurred())
				err = os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
				Expect(err).NotTo(HaveOccurred())

				// First, set up a node state with existing topology
				nodeState, err := spyreClient.Get(ctx, "node1")
				Expect(err).To(BeNil())
				nodeState.Spec.Pcitopo = `{"devices": {"0000:01:00.0": {}}, "version": 2.0}`
				_, err = spyreClient.Update(ctx, nodeState, false)
				Expect(err).To(BeNil())

				// Set PciTopology to nil and ensure metadata file is ignored
				spyretopo.PciTopology = nil
				_ = os.Setenv(spyretopo.IgnoreMetadataKey, "true")

				// Clear any dynamic topology file
				if utils.PathExists(spyretopo.DynamicTopologyFilepath) {
					_ = os.Remove(spyretopo.DynamicTopologyFilepath)
				}

				device := spyredevice.GeneratePseudoDevice("0000:99:00.0", resources.PfProductId)
				pciDevice := spyredevice.NewPseudoPciDevice(device)
				devices := []types.PciDevice{pciDevice}

				nodeState, err = spyredevice.WriteSpyreInterfacesToNodeState(ctx, cfg, devices, spyreClient, false, nil)
				Expect(err).To(BeNil())

				// When GetPciTopology fails and Pcitopo was not empty, it should be cleared
				Expect(nodeState.Spec.Pcitopo).To(Equal(""))

				err = os.Unsetenv(spyretopo.IgnoreMetadataKey)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("device management", func() {
			It("can accept Spyre devices", func() {
				devices := []*pci.Device{
					{
						Address: "0000:01:00.0",
						Vendor:  &pcidb.Vendor{ID: "vendorId", Name: "vendorName"},
						Product: &pcidb.Product{ID: "productId", Name: "productName"},
						Class:   &pcidb.Class{ID: "00"},
						Driver:  "testDriver",
					}, {
						Address: "0000:02:00.0",
						Vendor:  &pcidb.Vendor{ID: "vendorId", Name: "vendorName"},
						Product: &pcidb.Product{ID: "productId", Name: "productName"},
						Class:   &pcidb.Class{ID: "12"},
						Driver:  "testDriver",
					},
				}
				rf := factory.NewResourceFactory("ibm.com", "sock", utils.DetectPluginWatchMode(types.SockDir), "", spyreClient)
				dpList := map[types.DeviceType]types.DeviceProvider{types.SpyreDeviceType: spyredevice.NewSpyreDeviceProvider(rf)}
				for k, v := range types.SupportedDevices {
					dp, ok := dpList[k]
					Expect(ok).Should(BeTrue())
					err := dp.AddTargetDevices(devices, v)
					Expect(err).To(BeNil())
					Expect(len(dp.GetDiscoveredDevices())).Should(BeNumerically("==", 2))
				}

			})
			It("can ignore non-Spyre devices", func() {
				devices := []*pci.Device{
					{
						Address: "0000:01:00.0",
						Vendor:  &pcidb.Vendor{ID: "vendorId", Name: "vendorName"},
						Product: &pcidb.Product{ID: "productId", Name: "productName"},
						Class:   &pcidb.Class{ID: "99"}, // not supported!
						Driver:  "testDriver",
					},
				}
				rf := factory.NewResourceFactory("ibm.com", "sock", utils.DetectPluginWatchMode(types.SockDir), "", spyreClient)
				dpList := map[types.DeviceType]types.DeviceProvider{types.SpyreDeviceType: spyredevice.NewSpyreDeviceProvider(rf)}
				for k, v := range types.SupportedDevices {
					dp, ok := dpList[k]
					Expect(ok).Should(BeTrue())
					err := dp.AddTargetDevices(devices, v)
					Expect(err).To(BeNil())
					Expect(len(dp.GetDiscoveredDevices())).Should(BeNumerically("==", 0))
				}
			})
		})
	})

	Context("Allocation", Ordered, func() {
		var spyreClient *spyreclient.SpyreClient
		var err error
		var namespace string
		var node string

		BeforeEach(func() {
			namespace = createNewNamespace(ctx)
			node = "node1"
			_ = os.Setenv(spyredevice.NodeNameEnvKey, node)
			spyreClient, err = spyreclient.NewClient(context.Background(), cfg)
			Expect(err).To(BeNil())
			Expect(spyreClient).NotTo((BeNil()))
			s := &spyrev1alpha1.SpyreNodeState{
				ObjectMeta: metav1.ObjectMeta{
					Name:      node,
					Namespace: namespace,
				},
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					NodeName: node,
					SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
						{PciAddress: "01", NumVfs: 1},
						{PciAddress: "02", NumVfs: 1},
					},
				},
			}
			ctx := context.Background()
			_, err := spyreClient.Create(ctx, s)
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			ctx := context.Background()
			err = spyreClient.DeleteAll(ctx)
			Expect(err).To(BeNil())
		})
		oneNDev := int32(1)
		DescribeTable("classic allocation",
			func(allocations map[string]bool, requested []string, nDev int32, expectedSelection []string) {
				err := os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
				Expect(err).NotTo(HaveOccurred())
				deviceMap := make(map[string]*pluginapi.Device)
				for dev, allocated := range allocations {
					if !allocated {
						deviceMap[dev] = &pluginapi.Device{
							ID: dev,
						}
					}
				}
				selectedDeviceIdList := spyredevice.AllocateFromDeviceMap(requested, nDev, deviceMap)
				Expect(selectedDeviceIdList).To(Equal(expectedSelection))
			},
			Entry("request at empty allocation",
				map[string]bool{"01": false, "02": false}, []string{"01"}, oneNDev, []string{"01"}),
			Entry("request at empty allocation with alternative devices",
				map[string]bool{"01": false, "02": false}, []string{"01", "02"}, oneNDev, []string{"01"}),
			Entry("request at some device allocated",
				map[string]bool{"01": true, "02": false}, []string{"01", "02"}, oneNDev, []string{"02"}),
			Entry("request at all devices allocated",
				map[string]bool{"01": true, "02": true}, []string{"01", "02"}, oneNDev, nil),
		)

		DescribeTable("reservation-based allocation",
			func(before *spyrev1alpha1.SpyreNodeStateStatus, resourceName string,
				requestedDevices []string, nDev int32, expectedSelection []string,
				expectedErrorMessage string,
			) {
				ctx := context.Background()
				_ = os.Setenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				// set "before"
				s1, err := spyreClient.Get(ctx, node)
				Expect(err).To(BeNil())
				s1.Status = *before
				_, err = spyreClient.UpdateStatus(ctx, s1, false)
				Expect(err).To(BeNil())

				// check "before"
				s2, err := spyreClient.Get(ctx, node)
				Expect(err).To(BeNil())
				Expect(s2.Status).Should(Equal(s1.Status))

				// allocate
				selectedDeviceIdList, err := spyredevice.AllocateReservedDevices(
					ctx, spyreClient, resourceName, requestedDevices, nDev,
				)
				if expectedErrorMessage == "" {
					Expect(err).To(BeNil())
				} else {
					Expect(err).NotTo(BeNil())
					Expect(strings.Contains(err.Error(), expectedErrorMessage)).To(BeTrue())
				}
				Expect(selectedDeviceIdList).To(Equal(expectedSelection))
			},
			Entry("no reservation causes error",
				&spyrev1alpha1.SpyreNodeStateStatus{}, "spyre_pf", []string{"00", "01"},
				int32(2), nil, "unable to find reservation for resource"),
			Entry("different reservation causes error",
				&spyrev1alpha1.SpyreNodeStateStatus{
					Reservations: map[string]spyrev1alpha1.Reservation{
						"spyre_hello": {
							PodsUnderScheduling: []spyrev1alpha1.Pod{{Namespace: "n1", Name: "p1"}},
							DeviceSets:          [][]string{{"00", "01"}},
						},
					}}, "spyre_pf", []string{"00", "01"}, int32(2), nil, "unable to find reservation for resource"),
			Entry("one deviceSet will be allocated",
				&spyrev1alpha1.SpyreNodeStateStatus{
					Reservations: map[string]spyrev1alpha1.Reservation{
						"spyre_pf": {
							PodsUnderScheduling: []spyrev1alpha1.Pod{{Namespace: "n1", Name: "p1"}},
							DeviceSets:          [][]string{{"00", "01"}},
						},
					}}, "spyre_pf", []string{"00", "01"}, int32(2), []string{"00", "01"}, ""),
			Entry("one of deviceSets will be allocated",
				&spyrev1alpha1.SpyreNodeStateStatus{
					Reservations: map[string]spyrev1alpha1.Reservation{
						"spyre_pf": {
							PodsUnderScheduling: []spyrev1alpha1.Pod{
								{Namespace: "n1", Name: "p1"},
								{Namespace: "n1", Name: "p2"}},
							DeviceSets: [][]string{{"00", "01"}, {"02", "03"}},
						},
					}}, "spyre_pf", []string{"00", "01", "02", "03"}, int32(2), []string{"00", "01"}, ""),
			Entry("one of spyre_pf_tier0 deviceSets will be allocated",
				&spyrev1alpha1.SpyreNodeStateStatus{
					Reservations: map[string]spyrev1alpha1.Reservation{
						"spyre_pf": {
							PodsUnderScheduling: []spyrev1alpha1.Pod{{Namespace: "n1", Name: "p1"}},
							DeviceSets:          [][]string{{"02", "03"}},
						},
						"spyre_pf_tier0": {
							PodsUnderScheduling: []spyrev1alpha1.Pod{{Namespace: "n1", Name: "p2"}},
							DeviceSets:          [][]string{{"00", "01"}},
						},
					}}, "spyre_pf_tier0", []string{"00", "01"}, int32(2), []string{"00", "01"}, ""),
		)
	})
})

var _ = BeforeSuite(func() {

	var err error

	By("bootstrapping test environment")
	crdPath := filepath.Join("..", "..", "config", "crd", "external")
	_, err = os.Stat(crdPath)
	Expect(err).To(
		BeNil(),
		"%v not exist; spyre-operator must exists the same directory of the device plugin code",
		crdPath)
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// create namespace "test"
	k8sClient, err = client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	ns := &corev1.Namespace{}
	ns.Name = "test"
	err = k8sClient.Create(context.Background(), ns)
	Expect(err).NotTo(HaveOccurred())

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
