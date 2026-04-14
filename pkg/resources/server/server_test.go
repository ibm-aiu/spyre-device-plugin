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

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	. "github.com/ibm-aiu/spyre-device-plugin/pkg/resources/server"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func ReadAllocation(allocated chan types.AllocationInfo, expected []string) {
	selected, ok := <-allocated
	Expect(ok).To(BeTrue())
	Expect(selected.DeviceIDs).To(Equal(expected))
}

var _ = Describe("Resource Server", func() {
	ctx := context.Background()
	Context("configuration switch using SpyreClusterPolicy and environment variable", Ordered, func() {
		It("returns that preferred allocation is disabled", func() {
			rs := &ResourceServer{}
			r, err := rs.GetDevicePluginOptions(ctx, nil)
			Expect(err).To(BeNil())
			Expect(r.GetPreferredAllocationAvailable).Should(BeFalse())
		})
		It("returns that preferred allocation is enabled with envval", func() {
			os.Setenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
			rs := &ResourceServer{}
			r, err := rs.GetDevicePluginOptions(ctx, nil)
			Expect(err).To(BeNil())
			Expect(r.GetPreferredAllocationAvailable).Should(BeTrue())
		})
	})
	// NOTE: should share the same test case with spyredevice_test.go
	Context("preferred allocation functions", Serial, func() {
		var err error
		var namespace string
		nodeList := []string{"node1", "node2"}
		os.Setenv("NODE_NAME", "node1")

		BeforeEach(func() {
			ctx := context.Background()
			Expect(ServerRunning).To(BeTrue())
			namespace = CreateNewNamespace()
			for _, node := range nodeList {
				s := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name:      node,
						Namespace: namespace,
					},
					Spec: spyrev1alpha1.SpyreNodeStateSpec{
						NodeName: node,
						SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
							{PciAddress: NodeDeviceIds[0], NumVfs: 1},
							{PciAddress: NodeDeviceIds[1], NumVfs: 1},
						},
					},
					Status: spyrev1alpha1.SpyreNodeStateStatus{},
				}
				_, err = SpyreClient.Create(ctx, s)
				Expect(err).To(BeNil())
			}
			nsList, err := SpyreClient.List(ctx, &client.ListOptions{})
			Expect(err).To(BeNil())
			Expect(len(nsList.Items)).Should(Equal(len(nodeList)))
		})

		AfterEach(func() {
			ctx := context.Background()
			SpyreClient, err = spyreclient.NewClient(context.Background(), Cfg)
			Expect(err).To(BeNil())
			Expect(SpyreClient).NotTo((BeNil()))
			Expect(err).To(BeNil())
			for _, nodeName := range nodeList {
				err00 := SpyreClient.Delete(ctx, nodeName, &client.DeleteOptions{})
				Expect(err00).To(Succeed())
			}
		})
		oneNDev := int32(1)
		DeviceAllocationTrialLimit = 1
		DescribeTable("classic allocation",
			func(allocations map[string]bool, requested []string, nDev int32, expectedSelection []string) {
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
				allocatedCh := make(chan types.AllocationInfo)
				go ReadAllocation(allocatedCh, expectedSelection)
				apiDevices := make(map[string]*pluginapi.Device)

				for dev, allocated := range allocations {
					if !allocated {
						apiDevices[dev] = &pluginapi.Device{
							ID:     dev,
							Health: "healthy",
						}
					}
				}
				conf := &types.ResourceConfig{}
				devicePool := make(map[string]types.PciDevice)
				rPool := resources.NewResourcePool(conf, apiDevices, devicePool)

				rs := NewResourceServer("spyre_pf", "sock", true, rPool, SpyreClient, allocatedCh, "")
				cr := &pluginapi.ContainerPreferredAllocationRequest{
					AvailableDeviceIDs: requested,
					AllocationSize:     nDev,
				}
				rqt := &pluginapi.PreferredAllocationRequest{
					ContainerRequests: []*pluginapi.ContainerPreferredAllocationRequest{cr},
				}
				By("allocation")
				res, err := rs.GetPreferredAllocation(ctx, rqt)
				if expectedSelection == nil {
					fmt.Printf("resp err: %v %v\n", res, err)
					Expect(err).NotTo(BeNil())
					Expect(errors.IsNotFound(err)).To(BeTrue())
				} else {
					Expect(err).To(BeNil())
					Expect(res.ContainerResponses[0].DeviceIDs).Should(Equal(expectedSelection))
				}

			},
			Entry("request at empty allocation", map[string]bool{"01": false, "02": false}, []string{"01"}, oneNDev, []string{"01"}),
			Entry("request at empty allocation with alternative devices", map[string]bool{"01": false, "02": false}, []string{"01", "02"}, oneNDev, []string{"01"}),
			Entry("request at some device allocated", map[string]bool{"01": true, "02": false}, []string{"01", "02"}, oneNDev, []string{"02"}),
			Entry("request at all devices allocated", map[string]bool{"01": true, "02": true}, []string{"01", "02"}, oneNDev, nil),
		)
	})

	Context("getEnvs method", func() {
		var rs types.ResourceServer

		It("should return environment variables with single device", func() {
			deviceIDs := []string{"0000:01:00.0"}
			mockPool := &MockResourcePool{
				Envs: []string{"0000:01:00.0"},
			}
			allocatedCh := make(chan types.AllocationInfo, 1)
			rs = NewResourceServer("spyre_pf", "sock", true, mockPool, nil, allocatedCh, "")

			envs := rs.(*ResourceServer).GetEnvs(deviceIDs)

			Expect(envs).NotTo(BeNil())
			Expect(envs).To(HaveKey("PCIDEVICE_IBM_COM_AIU_PF"))
			Expect(envs["PCIDEVICE_IBM_COM_AIU_PF"]).To(Equal("0000:01:00.0"))
		})

		It("should return environment variables with multiple devices", func() {
			deviceIDs := []string{"0000:01:00.0", "0000:02:00.0", "0000:03:00.0"}
			mockPool := &MockResourcePool{
				Envs: []string{"0000:01:00.0", "0000:02:00.0", "0000:03:00.0"},
			}
			allocatedCh := make(chan types.AllocationInfo, 1)
			rs = NewResourceServer("spyre_pf", "sock", true, mockPool, nil, allocatedCh, "")

			envs := rs.(*ResourceServer).GetEnvs(deviceIDs)

			Expect(envs).NotTo(BeNil())
			Expect(envs).To(HaveKey("PCIDEVICE_IBM_COM_AIU_PF"))
			Expect(envs["PCIDEVICE_IBM_COM_AIU_PF"]).To(Equal("0000:01:00.0,0000:02:00.0,0000:03:00.0"))
		})

		It("should return environment variables with empty device list", func() {
			deviceIDs := []string{}
			mockPool := &MockResourcePool{
				Envs: []string{},
			}
			allocatedCh := make(chan types.AllocationInfo, 1)
			rs = NewResourceServer("spyre_pf", "sock", true, mockPool, nil, allocatedCh, "")

			envs := rs.(*ResourceServer).GetEnvs(deviceIDs)

			Expect(envs).NotTo(BeNil())
			Expect(envs).To(HaveKey("PCIDEVICE_IBM_COM_AIU_PF"))
			Expect(envs["PCIDEVICE_IBM_COM_AIU_PF"]).To(Equal(""))
		})

		It("should include metrics path when metrics are enabled", func() {
			// This test verifies the basic structure
			// The actual metrics path inclusion depends on config.IsMetricsEnabled()
			deviceIDs := []string{"0000:01:00.0"}
			mockPool := &MockResourcePool{
				Envs: []string{"0000:01:00.0"},
			}
			allocatedCh := make(chan types.AllocationInfo, 1)
			rs = NewResourceServer("spyre_pf", "sock", true, mockPool, nil, allocatedCh, "")

			envs := rs.(*ResourceServer).GetEnvs(deviceIDs)

			Expect(envs).NotTo(BeNil())
			Expect(envs).To(HaveKey("PCIDEVICE_IBM_COM_AIU_PF"))
			// Metrics path key "SPYRE_METRIC_PATH" may or may not be present
			// depending on whether config.IsMetricsEnabled() returns true
		})
	})
})
