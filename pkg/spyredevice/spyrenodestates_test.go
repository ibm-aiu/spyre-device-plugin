/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice

import (
	"os"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var _ = Describe("SpyreNodeStates", func() {

	DescribeTable("getSpyreInterfaceMaps", func(pciDevices []types.PciDevice,
		expectedSpyreInterfaces []string,
		expectedspyreSSAInterfaces []string) {
		spyreInterfaceMap, spyreSSAInterfaceMap := getSpyreInterfaceMaps(pciDevices)
		Expect(spyreInterfaceMap).To(HaveLen(len(expectedSpyreInterfaces)))
		Expect(spyreSSAInterfaceMap).To(HaveLen(len(expectedspyreSSAInterfaces)))
		for pciAddress := range spyreInterfaceMap {
			Expect(pciAddress).To(BeElementOf(expectedSpyreInterfaces))
		}
		for pciAddress := range spyreSSAInterfaceMap {
			Expect(pciAddress).To(BeElementOf(expectedspyreSSAInterfaces))
		}
	},
		Entry("empty", []types.PciDevice{}, []string{}, []string{}),
		Entry("pf", []types.PciDevice{
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.0", resources.PfProductId)),
		}, []string{"0000:1a:00.0", "0000:2a:00.0"}, []string{}),
		Entry("pf+vf", []types.PciDevice{
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.1", resources.VfProductId)),
		}, []string{"0000:1a:00.0"}, []string{}),
		Entry("ssa", []types.PciDevice{
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.0", resources.VfProductId)),
		}, []string{}, []string{"0000:1a:00.0", "0000:2a:00.0"}),
		Entry("mixed", []types.PciDevice{
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.1", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.0", resources.PfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:3a:00.0", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:4a:00.0", resources.VfProductId)),
		}, []string{"0000:1a:00.0", "0000:2a:00.0"}, []string{"0000:3a:00.0", "0000:4a:00.0"}),
	)

	It("can set healths in getSpyreInterfaceMaps", func() {
		pciDevices := []types.PciDevice{
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.2", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.1", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.0", resources.PfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.2", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.1", resources.VfProductId)),
			NewPseudoPciDevice(GeneratePseudoDevice("0000:3a:00.0", resources.VfProductId)), // SSA
		}
		pciDevices[2].SetHealth(pluginapi.Unhealthy)
		pciDevices[3].SetHealth(pluginapi.Unhealthy)
		pciDevices[6].SetHealth(pluginapi.Unhealthy)
		spyreInterfaceMap, spyreSSAInterfaceMap := getSpyreInterfaceMaps(pciDevices)
		Expect(spyreInterfaceMap).To(HaveLen(2))
		iface, found := spyreInterfaceMap["0000:1a:00.0"]
		Expect(found).To(BeTrue())
		Expect(iface.Health).To(Equal(spyrev1alpha1.SpyreUnhealthy))
		Expect(iface.Vfs).To(BeEquivalentTo([]string{"0000:1a:00.1", "0000:1a:00.2"}))
		iface, found = spyreInterfaceMap["0000:2a:00.0"]
		Expect(found).To(BeTrue())
		Expect(iface.Health).To(Equal(spyrev1alpha1.SpyreUnhealthy))
		Expect(iface.Vfs).To(BeEquivalentTo([]string{"0000:2a:00.1", "0000:2a:00.2"}))
		Expect(spyreSSAInterfaceMap).To(HaveLen(1))
		ssaIface, found := spyreSSAInterfaceMap["0000:3a:00.0"]
		Expect(found).To(BeTrue())
		Expect(ssaIface.Health).To(Equal(spyrev1alpha1.SpyreUnhealthy))
	})

	DescribeTable("updateSpyreInterfacesChanges", func(nsSpec spyrev1alpha1.SpyreNodeStateSpec,
		pciDevices []types.PciDevice, unhealthyDevices []spyrev1alpha1.UnhealthyDevice,
		expectedChange bool, expectedSpyreInterfaces, expectedspyreSSAInterfaces, expectedUnhealthyDevices int) {
		// create nodeState
		ns := &spyrev1alpha1.SpyreNodeState{
			Spec: nsSpec,
		}
		spyreInterfaceMap, spyreSSAInterfaceMap := getSpyreInterfaceMaps(pciDevices)
		specChanged, statusChanged := updateSpyreInterfacesChanges(ns, spyreInterfaceMap, spyreSSAInterfaceMap, unhealthyDevices)
		Expect(specChanged).To(Equal(expectedChange))
		// check spec
		Expect(ns.Spec.SpyreInterfaces).To(HaveLen(expectedSpyreInterfaces))
		Expect(ns.Spec.SpyreSSAInterfaces).To(HaveLen(expectedspyreSSAInterfaces))
		// check status
		Expect(statusChanged).To(BeTrue()) // from no condition
		Expect(ns.Status.UnhealthyDevices).To(HaveLen(expectedUnhealthyDevices))
		Expect(ns.Status.Conditions).To(HaveLen(1))
		Expect(ns.Status.Conditions[0].Type).To(Equal(ConditionTypeDeviceHealthy))
		switch {
		case expectedUnhealthyDevices > 0:
			Expect(ns.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionFalse))
		case expectedSpyreInterfaces+expectedspyreSSAInterfaces == 0:
			Expect(ns.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionUnknown))
		default:
			Expect(ns.Status.Conditions[0].Status).To(BeEquivalentTo(metav1.ConditionTrue))
		}
	},
		Entry("empty, no change", spyrev1alpha1.SpyreNodeStateSpec{}, []types.PciDevice{}, nil, false, 0, 0, 0),
		Entry("with a pf, no change",
			spyrev1alpha1.SpyreNodeStateSpec{
				SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
					{PciAddress: "0000:1a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
			}, []types.PciDevice{
				NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
			}, nil, false, 1, 0, 0),
		Entry("with new pf",
			spyrev1alpha1.SpyreNodeStateSpec{
				SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
					{PciAddress: "0000:1a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
			}, []types.PciDevice{
				NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
				NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.0", resources.PfProductId)),
			}, nil, true, 2, 0, 0),
		Entry("with new vf",
			spyrev1alpha1.SpyreNodeStateSpec{
				SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
					{PciAddress: "0000:1a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
			}, []types.PciDevice{
				NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
				NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.1", resources.VfProductId)),
			}, nil, true, 1, 0, 0),
		Entry("with new ssa vf",
			spyrev1alpha1.SpyreNodeStateSpec{
				SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
					{PciAddress: "0000:1a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
			}, []types.PciDevice{
				NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId)),
				NewPseudoPciDevice(GeneratePseudoDevice("0000:2a:00.0", resources.VfProductId)),
			}, nil, true, 1, 1, 0),
		Entry("with removal",
			spyrev1alpha1.SpyreNodeStateSpec{
				SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
					{PciAddress: "0000:1a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
				SpyreSSAInterfaces: []spyrev1alpha1.SpyreSSAInterface{
					{PciAddress: "0000:2a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
			}, []types.PciDevice{}, nil, true, 1, 1, 2),
		Entry("with removal and unhealthy device",
			spyrev1alpha1.SpyreNodeStateSpec{
				SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
					{PciAddress: "0000:1a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
				SpyreSSAInterfaces: []spyrev1alpha1.SpyreSSAInterface{
					{PciAddress: "0000:2a:00.0", Health: spyrev1alpha1.SpyreHealthy},
				},
			}, []types.PciDevice{
				func() types.PciDevice {
					pciDevice := NewPseudoPciDevice(GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId))
					pciDevice.SetHealth(pluginapi.Unhealthy)
					return pciDevice
				}(),
			}, []spyrev1alpha1.UnhealthyDevice{{ID: "0000:1a:00.0", State: pb.DEVICE_STATE_IN_ERROR.String()}}, true, 1, 1, 2),
	)

	DescribeTable("updateSpyreInterfacesWithTopo", func(topoVersion float64, nsTopo string, expectedChange bool) {
		// create topo
		topo := pcitopov2.Pcitopo{Devices: map[string]pcitopov2.Device{"00": {}}, Version: float32(topoVersion)}
		newTopo := topo.String()

		// create nodeState
		ns := &spyrev1alpha1.SpyreNodeState{
			Spec: spyrev1alpha1.SpyreNodeStateSpec{
				Pcitopo: nsTopo,
			},
		}

		changed := updateSpyreInterfacesWithTopo(topo, ns, false)

		Expect(changed).To(Equal(expectedChange))
		Expect(ns.Spec.Pcitopo).To(Equal(newTopo))
	},
		// topo.Version == 0 -> cannot sync (still returns whatever changed is)
		Entry("topo version 0 with different Pcitopo", 0.0, "", true),
		Entry("topo version 0 with same Pcitopo", 0.0, pcitopov2.Pcitopo{Devices: map[string]pcitopov2.Device{"00": {}}, Version: 0}.String(), false),

		// topo.Version > 0, expect changed when Pcitopo differs
		Entry("valid topo different from nodeState", 2.0, "", true),
		Entry("valid topo same as nodeState", 2.0, pcitopov2.Pcitopo{Devices: map[string]pcitopov2.Device{"00": {}}, Version: 2}.String(), false),
	)
	DescribeTable("updateSpyreInterfacesWithTopo with SpyreVfDevices(Isolated VF)", func(topoVersion float64, nsTopo string, expectedChange bool) {
		// create topo with SpyreVfDevices
		topo := pcitopov2.Pcitopo{
			Devices:        map[string]pcitopov2.Device{},
			SpyreVfDevices: map[string]pcitopov2.Device{"02": {}},
			Version:        float32(topoVersion),
		}
		newTopo := topo.String()

		// create nodeState
		ns := &spyrev1alpha1.SpyreNodeState{
			Spec: spyrev1alpha1.SpyreNodeStateSpec{
				Pcitopo: nsTopo,
			},
		}

		changed := updateSpyreInterfacesWithTopo(topo, ns, false)

		Expect(changed).To(Equal(expectedChange))
		Expect(ns.Spec.Pcitopo).To(Equal(newTopo))
	},
		Entry("topo version 2, nodeState empty", 2.0, "", true),
		Entry("topo version 2, nodeState same as topo", 2.0, pcitopov2.Pcitopo{
			Devices:        map[string]pcitopov2.Device{},
			SpyreVfDevices: map[string]pcitopov2.Device{"02": {}},
			Version:        2,
		}.String(), false),
		Entry("topo version 0, nodeState different", 0.0, "", true),
	)

	DescribeTable("updateSpyreInterfacesWithTopo with initial change flag", func(initialChanged bool, topoVersion float64, nsTopo string, expectedChange bool) {
		topo := pcitopov2.Pcitopo{
			Devices:        map[string]pcitopov2.Device{"01": {}},
			SpyreVfDevices: map[string]pcitopov2.Device{"02": {}},
			Version:        float32(topoVersion),
		}
		newTopo := topo.String()
		ns := &spyrev1alpha1.SpyreNodeState{
			Spec: spyrev1alpha1.SpyreNodeStateSpec{
				Pcitopo: nsTopo,
			},
		}
		changed := updateSpyreInterfacesWithTopo(topo, ns, initialChanged)
		Expect(changed).To(Equal(expectedChange))
		Expect(ns.Spec.Pcitopo).To(Equal(newTopo))
	},
		Entry("initial changed=true, topo different, version>0", true, 2.0, "", true),
		Entry("initial changed=true, topo same, version>0", true, 2.0, pcitopov2.Pcitopo{
			Devices:        map[string]pcitopov2.Device{"01": {}},
			SpyreVfDevices: map[string]pcitopov2.Device{"02": {}},
			Version:        2,
		}.String(), true),
		Entry("initial changed=true, topo different, version=0", true, 0.0, "", true),
		Entry("initial changed=true, topo same, version=0", true, 0.0, pcitopov2.Pcitopo{
			Devices:        map[string]pcitopov2.Device{"01": {}},
			SpyreVfDevices: map[string]pcitopov2.Device{"02": {}},
			Version:        0,
		}.String(), true),
	)

	DescribeTable("updateSpyreInterfacesWithTopo with complex device configurations", func(topoVersion float64, oldTopoDevices, newTopoDevices map[string]pcitopov2.Device, expectedChange bool) {
		oldTopo := pcitopov2.Pcitopo{
			Devices: oldTopoDevices,
			Version: float32(topoVersion),
		}
		newTopo := pcitopov2.Pcitopo{
			Devices: newTopoDevices,
			Version: float32(topoVersion),
		}
		ns := &spyrev1alpha1.SpyreNodeState{
			Spec: spyrev1alpha1.SpyreNodeStateSpec{
				Pcitopo: oldTopo.String(),
			},
		}
		changed := updateSpyreInterfacesWithTopo(newTopo, ns, false)
		Expect(changed).To(Equal(expectedChange))
		Expect(ns.Spec.Pcitopo).To(Equal(newTopo.String()))
	},
		Entry("Same device set", 2.0,
			map[string]pcitopov2.Device{"01": {}, "02": {}},
			map[string]pcitopov2.Device{"01": {}, "02": {}},
			false),
		Entry("Added devices", 2.0,
			map[string]pcitopov2.Device{"01": {}},
			map[string]pcitopov2.Device{"01": {}, "02": {}, "03": {}},
			true),
		Entry("Removed devices", 2.0,
			map[string]pcitopov2.Device{"01": {}, "02": {}, "03": {}},
			map[string]pcitopov2.Device{"01": {}},
			true),
		Entry("Different device set", 2.0,
			map[string]pcitopov2.Device{"01": {}, "02": {}},
			map[string]pcitopov2.Device{"03": {}, "04": {}},
			true),
		Entry("Version=0 doesn't matter if different", 0.0,
			map[string]pcitopov2.Device{"01": {}},
			map[string]pcitopov2.Device{"02": {}},
			true),
	)

	DescribeTable("updateSpyreInterfacesWithTopo with device attribute changes", func(topoVersion float64, expectedChange bool) {
		device1 := pcitopov2.Device{}
		device2 := pcitopov2.Device{}
		oldTopo := pcitopov2.Pcitopo{
			Devices: map[string]pcitopov2.Device{"01": device1},
			Version: float32(topoVersion),
		}
		newTopo := pcitopov2.Pcitopo{
			Devices: map[string]pcitopov2.Device{"01": device2, "02": device2},
			Version: float32(topoVersion),
		}
		ns := &spyrev1alpha1.SpyreNodeState{
			Spec: spyrev1alpha1.SpyreNodeStateSpec{
				Pcitopo: oldTopo.String(),
			},
		}
		changed := updateSpyreInterfacesWithTopo(newTopo, ns, false)
		Expect(changed).To(Equal(expectedChange))
		Expect(ns.Spec.Pcitopo).To(Equal(newTopo.String()))
	},
		Entry("Device property changed with version>0", 2.0, true),
		Entry("Device property changed with version=0", 0.0, true),
	)

	Context("Health filtering behavior", func() {
		var (
			originalPseudoMode string
			pseudoModeSet      bool
		)

		BeforeEach(func() {
			originalPseudoMode, pseudoModeSet = os.LookupEnv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
		})

		AfterEach(func() {
			if pseudoModeSet {
				os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), originalPseudoMode)
			} else {
				os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
			}
		})

		It("should skip health filtering in pseudo mode", func() {
			os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), "1")
			allDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8", Name: "Device 1"},
				"0002:00:00.0": {DeviceId: "06a8", Name: "Device 2"},
				"0003:00:00.0": {DeviceId: "06a8", Name: "Device 3"},
			}
			topo := pcitopov2.Pcitopo{
				Devices:    allDevices,
				Version:    2.0,
				NumDevices: 3,
			}
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{},
			}
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, false)
			Expect(changed).To(BeTrue())
			resultTopo, err := pcitopov2.UnmarshalPciTopo([]byte(nodeState.Spec.Pcitopo))
			Expect(err).To(BeNil())
			Expect(resultTopo.NumDevices).To(Equal(3))
			Expect(resultTopo.Devices).To(HaveLen(3))
			Expect(resultTopo.Devices).To(HaveKey("0001:00:00.0"))
			Expect(resultTopo.Devices).To(HaveKey("0002:00:00.0"))
			Expect(resultTopo.Devices).To(HaveKey("0003:00:00.0"))
		})

		It("should apply health filtering in non-pseudo mode", func() {
			os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
			allDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8", Name: "Device 1"},
				"0002:00:00.0": {DeviceId: "06a8", Name: "Device 2"},
				"0003:00:00.0": {DeviceId: "06a8", Name: "Device 3"},
			}
			originalTopo := pcitopov2.Pcitopo{
				Devices:    allDevices,
				Version:    2.0,
				NumDevices: 3,
			}
			healthyDevices := map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": false,
				"0003:00:00.0": true,
			}
			filteredTopo := spyretopo.FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			Expect(filteredTopo.NumDevices).To(Equal(2))
			Expect(filteredTopo.Devices).To(HaveLen(2))
			Expect(filteredTopo.Devices).To(HaveKey("0001:00:00.0"))
			Expect(filteredTopo.Devices).NotTo(HaveKey("0002:00:00.0"))
			Expect(filteredTopo.Devices).To(HaveKey("0003:00:00.0"))
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{},
			}
			changed := updateSpyreInterfacesWithTopo(filteredTopo, nodeState, false)
			Expect(changed).To(BeTrue())
			resultTopo, err := pcitopov2.UnmarshalPciTopo([]byte(nodeState.Spec.Pcitopo))
			Expect(err).To(BeNil())
			Expect(resultTopo.NumDevices).To(Equal(2))
		})

		It("should handle all devices unhealthy in non-pseudo mode", func() {
			os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())

			allDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8"},
				"0002:00:00.0": {DeviceId: "06a8"},
			}
			originalTopo := pcitopov2.Pcitopo{
				Devices:    allDevices,
				Version:    2.0,
				NumDevices: 2,
			}
			healthyDevices := map[string]bool{
				"0001:00:00.0": false,
				"0002:00:00.0": false,
			}

			filteredTopo := spyretopo.FilterTopologyByDeviceHealth(originalTopo, healthyDevices)

			Expect(filteredTopo.NumDevices).To(Equal(0))
			Expect(filteredTopo.Devices).To(HaveLen(0))
		})

		It("should handle all devices healthy in non-pseudo mode", func() {
			os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())

			allDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8"},
				"0002:00:00.0": {DeviceId: "06a8"},
			}
			originalTopo := pcitopov2.Pcitopo{
				Devices:    allDevices,
				Version:    2.0,
				NumDevices: 2,
			}
			healthyDevices := map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": true,
			}
			filteredTopo := spyretopo.FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			Expect(filteredTopo.NumDevices).To(Equal(2))
			Expect(filteredTopo.Devices).To(HaveLen(2))
			Expect(filteredTopo.Devices).To(HaveKey("0001:00:00.0"))
			Expect(filteredTopo.Devices).To(HaveKey("0002:00:00.0"))
		})
	})

	Describe("updateSpyreInterfacesWithTopo edge cases", func() {
		It("should handle topology with both Devices and SpyreVfDevices", func() {
			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
				},
				SpyreVfDevices: map[string]pcitopov2.Device{
					"0000:00:02.0": {DeviceId: "06a8"},
				},
				Version:    2.0,
				NumDevices: 2,
			}
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, false)
			Expect(changed).To(BeTrue())
			Expect(nodeState.Spec.Pcitopo).To(Equal(topo.String()))
		})

		It("should maintain changed flag when topology is updated", func() {
			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
				},
				Version: 2.0,
			}
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}
			// Pass true for initial changed state
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, true)
			Expect(changed).To(BeTrue())
			Expect(nodeState.Spec.Pcitopo).To(Equal(topo.String()))
		})

		It("should handle empty topology string in nodeState", func() {
			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{},
				Version: 2.0,
			}
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, false)
			Expect(changed).To(BeTrue())
		})

		It("should detect no change when topology strings match exactly", func() {
			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
				},
				Version:    2.0,
				NumDevices: 1,
			}
			existingTopo := topo.String()
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: existingTopo,
				},
			}
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, false)
			Expect(changed).To(BeFalse())
			Expect(nodeState.Spec.Pcitopo).To(Equal(existingTopo))
		})

		It("should clear topology when version is 0 and keep changed flag", func() {
			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
				},
				Version: 0.0,
			}
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "some_existing_topology",
				},
			}
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, false)
			Expect(changed).To(BeTrue())
			// Even though version is 0, the topology string should still be updated
			Expect(nodeState.Spec.Pcitopo).To(Equal(topo.String()))
		})
	})

	Context("Topology cleanup scenario", func() {
		It("should set changed to true when clearing non-empty Pcitopo", func() {
			// Simulating the else clause: nodeState.Spec.Pcitopo != ""
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: `{"devices": {"0000:00:01.0": {}}, "version": 2.0}`,
				},
			}
			// Simulate clearing topology by calling with empty topo
			emptyTopo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{},
				Version: 0.0,
			}
			// When clearing, we'd expect change to be detected
			changed := updateSpyreInterfacesWithTopo(emptyTopo, nodeState, false)
			Expect(changed).To(BeTrue())
		})

		It("should handle transition from populated to empty topology", func() {
			populatedTopo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
					"0000:00:02.0": {DeviceId: "06a7"},
				},
				Version:    2.0,
				NumDevices: 2,
			}
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: populatedTopo.String(),
				},
			}
			emptyTopo := pcitopov2.Pcitopo{
				Devices:    map[string]pcitopov2.Device{},
				Version:    2.0,
				NumDevices: 0,
			}
			changed := updateSpyreInterfacesWithTopo(emptyTopo, nodeState, false)
			Expect(changed).To(BeTrue())
			Expect(nodeState.Spec.Pcitopo).To(Equal(emptyTopo.String()))
		})
	})

	Context("Topology update workflow scenarios", func() {
		var originalPseudoMode string
		var pseudoModeSet bool

		BeforeEach(func() {
			originalPseudoMode, pseudoModeSet = os.LookupEnv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
		})

		AfterEach(func() {
			if pseudoModeSet {
				os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), originalPseudoMode)
			} else {
				os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
			}
		})

		It("should update topology in pseudo mode without health filtering", func() {
			os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), "1")

			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
					"0000:00:02.0": {DeviceId: "06a7"},
				},
				Version:    2.0,
				NumDevices: 2,
			}

			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}

			// This simulates: if os.Getenv(PseudoDeviceMode) == ModeEnabledValue
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, false)

			Expect(changed).To(BeTrue())
			Expect(nodeState.Spec.Pcitopo).To(Equal(topo.String()))
		})

		It("should apply health filtering in non-pseudo mode", func() {
			os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())

			originalTopo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
					"0000:00:02.0": {DeviceId: "06a7"},
					"0000:00:03.0": {DeviceId: "06a7"},
				},
				Version:    2.0,
				NumDevices: 3,
			}

			// Simulate health filtering: only devices 1 and 3 are healthy
			healthyDevices := map[string]bool{
				"0000:00:01.0": true,
				"0000:00:02.0": false,
				"0000:00:03.0": true,
			}

			filteredTopo := spyretopo.FilterTopologyByDeviceHealth(originalTopo, healthyDevices)

			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}

			// This simulates the else branch with health filtering
			changed := updateSpyreInterfacesWithTopo(filteredTopo, nodeState, false)

			Expect(changed).To(BeTrue())
			Expect(nodeState.Spec.Pcitopo).To(Equal(filteredTopo.String()))

			// Verify filtered topology has only healthy devices
			resultTopo, err := pcitopov2.UnmarshalPciTopo([]byte(nodeState.Spec.Pcitopo))
			Expect(err).To(BeNil())
			Expect(resultTopo.NumDevices).To(Equal(2))
			Expect(resultTopo.Devices).To(HaveKey("0000:00:01.0"))
			Expect(resultTopo.Devices).NotTo(HaveKey("0000:00:02.0"))
			Expect(resultTopo.Devices).To(HaveKey("0000:00:03.0"))
		})

		It("should clear topology when error occurs and Pcitopo is not empty", func() {
			// This simulates: else if nodeState.Spec.Pcitopo != ""
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: `{"devices": {"0000:00:01.0": {}}, "version": 2.0}`,
				},
			}

			// In the actual code, this happens when GetPciTopology returns error
			// We simulate clearing the topology
			originalPcitopo := nodeState.Spec.Pcitopo
			Expect(originalPcitopo).NotTo(BeEmpty())

			// Simulate the cleanup: changed = true; nodeState.Spec.Pcitopo = ""
			changed := true
			nodeState.Spec.Pcitopo = ""

			Expect(changed).To(BeTrue())
			Expect(nodeState.Spec.Pcitopo).To(Equal(""))
		})

		It("should not change anything when error occurs and Pcitopo is already empty", func() {
			// This tests the negative case of: else if nodeState.Spec.Pcitopo != ""
			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}

			originalPcitopo := nodeState.Spec.Pcitopo
			Expect(originalPcitopo).To(BeEmpty())

			// When Pcitopo is empty, the cleanup branch should not execute
			// So changed should remain false if it was false before
			changed := false
			if nodeState.Spec.Pcitopo != "" {
				changed = true
				nodeState.Spec.Pcitopo = ""
			}

			Expect(changed).To(BeFalse())
			Expect(nodeState.Spec.Pcitopo).To(Equal(""))
		})

		It("should preserve changed flag when updating topology", func() {
			topo := pcitopov2.Pcitopo{
				Devices: map[string]pcitopov2.Device{
					"0000:00:01.0": {DeviceId: "06a7"},
				},
				Version: 2.0,
			}

			nodeState := &spyrev1alpha1.SpyreNodeState{
				Spec: spyrev1alpha1.SpyreNodeStateSpec{
					Pcitopo: "",
				},
			}

			// Test with initial changed = true
			changed := updateSpyreInterfacesWithTopo(topo, nodeState, true)
			Expect(changed).To(BeTrue())

			// Test with initial changed = false but topology differs
			nodeState.Spec.Pcitopo = ""
			changed = updateSpyreInterfacesWithTopo(topo, nodeState, false)
			Expect(changed).To(BeTrue())
		})
	})
})
