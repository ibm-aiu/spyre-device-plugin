/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package manager_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ibm-aiu/spyre-device-plugin/cmd/spyre/manager"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	pfConfigFileName   = "config.json"
	vfConfigFileName   = "vf_config.json"
	defaultPfPoolNames = []string{"spyre_pf"}
	perDevicePoolNames = []string{"spyre_pf_0000_1a_00.0", "spyre_pf_0000_1c_00.0", "spyre_pf_0000_1d_00.0", "spyre_pf_0000_1e_00.0",
		"spyre_pf_0000_3d_00.0", "spyre_pf_0000_3f_00.0", "spyre_pf_0000_40_00.0", "spyre_pf_0000_41_00.0"}
	vfPerDevicePoolNames = []string{"spyre_vf_0000_1a_00.0", "spyre_vf_0000_1c_00.0", "spyre_vf_0000_1d_00.0", "spyre_vf_0000_1e_00.0",
		"spyre_vf_0000_3d_00.0", "spyre_vf_0000_3f_00.0", "spyre_vf_0000_40_00.0", "spyre_vf_0000_41_00.0"}
	isolatedVfPerDevicePoolNames = []string{"spyre_vf_0001_00_00.0", "spyre_vf_0002_00_00.0", "spyre_vf_0003_00_00.0", "spyre_vf_0004_00_00.0",
		"spyre_vf_0005_00_00.0", "spyre_vf_0006_00_00.0", "spyre_vf_0007_00_00.0", "spyre_vf_0008_00_00.0"}
	topologyAwarePoolNames   = []string{"spyre_pf_tier0", "spyre_pf_tier1", "spyre_pf_tier2"}
	vfTopologyAwarePoolNames = []string{"spyre_vf_tier0", "spyre_vf_tier1", "spyre_vf_tier2"}
	vfEnabledPoolNames       = []string{"spyre_pf", "spyre_vf"}
)

var _ = Describe("Manager", Ordered, func() {

	BeforeAll(func() {
		os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), spyreconst.ModeEnabledValue)
		if utils.PathExists(spyretopo.DynamicTopologyFilepath) {
			os.Remove(spyretopo.DynamicTopologyFilepath)
		}
		spyretopo.PciTopology = nil
	})

	AfterAll(func() {
		os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
		if utils.PathExists(spyretopo.DynamicTopologyFilepath) {
			os.Remove(spyretopo.DynamicTopologyFilepath)
		}
		spyretopo.PciTopology = nil
	})

	Context("with topology file", func() {
		BeforeEach(func() {
			os.Setenv(spyrev1alpha1.ReservationMode.EnvKey(), spyreconst.ModeEnabledValue)
		})
		AfterEach(func() {
			os.Unsetenv(spyrev1alpha1.ReservationMode.EnvKey())
		})

		Context("physical devices", Ordered, func() {
			BeforeEach(func() {
				os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
			})
			AfterEach(func() {
				os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
			})

			DescribeTable("can correctly advertise", func(perDevice bool, topologyAware bool, expectedPoolNames []string) {
				if perDevice {
					os.Setenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				}
				if topologyAware {
					os.Setenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				}
				By("calling initManagerAndGetResourceServers")
				rsList := initManagerAndGetResourceServers(pfConfigFileName)
				Expect(rsList).Should(HaveLen(len(expectedPoolNames)))
				By("checking each resource pool")
				for _, rs := range rsList {
					rp := rs.GetResourcePool()
					Expect(rp.GetResourceName()).Should(BeElementOf(expectedPoolNames))
				}
			},
				Entry("spyre_pf only", false, false, defaultPfPoolNames),
				Entry("spyre_pf with perDeviceAllocation", true, false, append(defaultPfPoolNames, perDevicePoolNames...)),
				Entry("spyre_pf with topologyAwareAllocation", false, true, append(defaultPfPoolNames, topologyAwarePoolNames...)),
				Entry("spyre_pf with perDeviceAllocation and topologyAwareAllocation", true, true,
					append(append(defaultPfPoolNames, perDevicePoolNames...), topologyAwarePoolNames...)),
			)
		})

		Context("virtual devices", Ordered, func() {
			BeforeEach(func() {
				if runtime.GOARCH == "s390x" {
					Skip("Card Management VF are not supported on s390x")
				}
				os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
			})
			AfterEach(func() {
				os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
			})

			DescribeTable("GetPseudoVfAddress", func(pfAddress string, vfIndex int, expected string) {
				output := utils.GetPseudoVfAddress(pfAddress, vfIndex)
				Expect(output).To(Equal(expected))
			},
				Entry("index=1", "0000:1a:00.0", 1, "0000:1a:00.1"),
				Entry("index=2", "0000:1a:00.0", 2, "0000:1a:00.2"),
				Entry("index=0 (invalid)", "0000:1a:00.0", 0, ""),
				Entry("index=-1 (invalid)", "0000:1a:00.0", -1, ""),
			)
			DescribeTable("GetPseudoPfAddress", func(vfAddress string, expected string) {
				output := utils.GetPseudoPfAddress(vfAddress)
				Expect(output).To(Equal(expected))
			},
				Entry("pf", "0000:1a:00.0", ""),
				Entry("vf index=1", "0000:1a:00.1", "0000:1a:00.0"),
				Entry("vf index=2", "0000:1a:00.2", "0000:1a:00.0"),
				Entry("invalid vf", "0000:1a:00", ""),
			)

			DescribeTable("can correctly advertise", func(perDevice bool, topologyAware bool, expectedPoolNames []string) {
				if perDevice {
					os.Setenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				}
				if topologyAware {
					os.Setenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				}
				rsList := initManagerAndGetResourceServers(vfConfigFileName)
				Expect(rsList).Should(HaveLen(len(expectedPoolNames)))
				resourceMemo := make(map[string]interface{})
				for _, rs := range rsList {
					rp := rs.GetResourcePool()
					rn := rp.GetResourceName()
					Expect(rn).Should(BeElementOf(expectedPoolNames))
					_, found := resourceMemo[rn]
					Expect(found).To(BeFalse(), "resource in list must be unique")
					resourceMemo[rn] = nil
					devices := rp.GetDevices()
					if strings.Contains(rn, "spyre_vf") {
						if rn == "spyre_vf" || strings.Contains(rn, "_tier") {
							Expect(devices).Should(HaveLen(2 * 8))
						} else { // per-device
							Expect(devices).Should(HaveLen(2))
						}
						memo := make(map[string]interface{})
						for vfAddress := range devices {
							// check uniqueness
							_, found := memo[vfAddress]
							Expect(found).To(BeFalse(), "vf devices must be unique")
							memo[vfAddress] = nil
							splits := strings.Split(vfAddress, ".")
							subIndex := splits[len(splits)-1]
							Expect(subIndex).Should(BeElementOf("1", "2"))
						}
					}
				}
			},
				Entry("spyre_vf only", false, false, vfEnabledPoolNames),
				Entry("spyre_vf with perDeviceAllocation", true, false,
					append(append(vfEnabledPoolNames, perDevicePoolNames...), vfPerDevicePoolNames...)),
				Entry("spyre_vf with topologyAware", false, true,
					append(append(vfEnabledPoolNames, topologyAwarePoolNames...), vfTopologyAwarePoolNames...)),
				Entry("spyre_vf with perDeviceAllocation and topologyAware", true, true,
					append(append(append(append(vfEnabledPoolNames, perDevicePoolNames...), vfPerDevicePoolNames...),
						topologyAwarePoolNames...), vfTopologyAwarePoolNames...)),
			)

			It("can ignore vf resource", func() {
				os.Setenv(spyrev1alpha1.DisableVfMode.EnvKey(), spyreconst.ModeEnabledValue)
				rsList := initManagerAndGetResourceServers(vfConfigFileName)
				Expect(rsList).Should(HaveLen(1))
				for _, rs := range rsList {
					rp := rs.GetResourcePool()
					Expect(rp.GetResourceName()).Should(BeElementOf("spyre_pf"))
				}
			})
		})

		Context("Isolated VF", Ordered, func() {
			BeforeEach(func() {
				if runtime.GOARCH != "s390x" {
					Skip("Isolated VF are only supported on s390x")
				}
				os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.DisableVfMode.EnvKey())
			})

			AfterEach(func() {
				os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey())
				os.Unsetenv(spyrev1alpha1.DisableVfMode.EnvKey())
			})

			DescribeTable("can correctly advertise", func(perDevice bool, topologyAware bool, expectedPoolNames []string) {
				if perDevice {
					os.Setenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				}
				if topologyAware {
					os.Setenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey(), spyreconst.ModeEnabledValue)
				}
				rsList := initManagerAndGetResourceServers(vfConfigFileName)
				Expect(rsList).Should(HaveLen(len(expectedPoolNames)))
				resourceMemo := make(map[string]interface{})
				for _, rs := range rsList {
					rp := rs.GetResourcePool()
					rn := rp.GetResourceName()
					Expect(rn).Should(BeElementOf(expectedPoolNames))
					_, found := resourceMemo[rn]
					Expect(found).To(BeFalse(), "resource in list must be unique")
					resourceMemo[rn] = nil
					devices := rp.GetDevices()
					if strings.Contains(rn, "spyre_vf") {
						if rn == "spyre_vf" {
							Expect(devices).Should(HaveLen(8))
						} else { // per-device
							Expect(devices).Should(HaveLen(1))
						}
						memo := make(map[string]interface{})
						for vfAddress := range devices {
							// check uniqueness
							_, found := memo[vfAddress]
							Expect(found).To(BeFalse(), "vf devices must be unique")
							memo[vfAddress] = nil
							Expect(vfAddress).Should(MatchRegexp(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:00\.0$`))
						}
					}
				}
			},
				Entry("isolated spyre_vf only", false, false, vfEnabledPoolNames),
				Entry("isolated spye_vf with perDeviceAllocation", true, false,
					append(append(vfEnabledPoolNames, perDevicePoolNames...), isolatedVfPerDevicePoolNames...)),
			)
		})

	})

	Context("without topology file", func() {
		BeforeEach(func() {
			os.Unsetenv(spyrev1alpha1.ReservationMode.EnvKey())
			spyretopo.PciTopology = nil
			topofile := spyretopo.GetTopologyFile()
			Expect(topofile).To(BeEquivalentTo(""))
			_, err := spyretopo.GetPciTopology("", true)
			Expect(err).To(HaveOccurred())
		})
		It("must have devices", func() {
			rsList := initManagerAndGetResourceServers(pfConfigFileName)
			Expect(rsList).Should(HaveLen(1))
			rp := rsList[0].GetResourcePool()
			rn := rp.GetResourceName()
			Expect(rn).Should(BeElementOf(defaultPfPoolNames))
			devices := rp.GetDevices()
			Expect(devices).To(HaveLen(8))
		})
	})

	Context("hotplug functionality", func() {
		var rm *manager.ResourceManager

		BeforeEach(func() {
			os.Setenv(spyrev1alpha1.ReservationMode.EnvKey(), spyreconst.ModeEnabledValue)

			cp := &manager.CliParams{
				ConfigFile:     filepath.Join("..", "..", "..", "test", "data", pfConfigFileName),
				ResourcePrefix: "ibm.com",
				ProbePort:      "0", // port will be chosen by OS
			}
			// Use the test config from the suite setup
			rm = manager.ExportNewResourceManagerWithConfig(cp, nil, testCfg)
			rm.StopProbeManager()
			err := rm.ReadConfig()
			Expect(err).To(BeNil())
			result := rm.ValidConfigs()
			Expect(result).To(BeTrue())
			err = rm.DiscoverHostDevices()
			Expect(err).To(BeNil())
			err = rm.InitServers()
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			if rm != nil {
				rm.StopProbeManager()
			}
			os.Unsetenv(spyrev1alpha1.ReservationMode.EnvKey())
			os.Unsetenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey())
		})

		Describe("addNewDevicesToProvider", func() {
			It("should return error when devices don't exist in system", func() {
				if runtime.GOOS == "darwin" {
					Skip("addNewDevicesToProvider tests are not supported on darwin")
				}
				newDevices := createMockPciDevices([]string{"0009:00:00.0", "000a:00:00.0"})
				err := rm.TestAddNewDevicesToProvider(newDevices)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no valid GHW devices found from new devices"))
			})

			It("should handle empty device list", func() {
				if runtime.GOOS == "darwin" {
					Skip("addNewDevicesToProvider tests are not supported on darwin")
				}
				newDevices := []types.PciDevice{}
				err := rm.TestAddNewDevicesToProvider(newDevices)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no valid GHW devices found"))
			})
		})

	})

	Context("containsPfOrVfDevices helper function", func() {
		DescribeTable("should correctly identify device types",
			func(addresses []string, vfAddresses []string, expectedPf bool, expectedVf bool) {
				var allDevices []types.PciDevice
				if len(addresses) > 0 {
					pfDevices := createMockPciDevices(addresses)
					allDevices = append(allDevices, pfDevices...)
				}
				if len(vfAddresses) > 0 {
					vfDevices := createMockVfPciDevices(vfAddresses)
					allDevices = append(allDevices, vfDevices...)
				}

				isPf, isVf := manager.ExportContainsPfOrVfDevices(allDevices)
				Expect(isPf).To(Equal(expectedPf))
				Expect(isVf).To(Equal(expectedVf))
			},
			Entry("only PF devices", []string{"0009:00:00.0", "000a:00:00.0"}, []string{}, true, false),
			Entry("only VF devices", []string{}, []string{"0009:00:00.1", "000a:00:00.1"}, false, true),
			Entry("mixed PF and VF devices", []string{"0009:00:00.0"}, []string{"000a:00:00.1"}, true, true),
			Entry("empty device list", []string{}, []string{}, false, false),
			Entry("single PF device", []string{"0009:00:00.0"}, []string{}, true, false),
			Entry("single VF device", []string{}, []string{"0009:00:00.1"}, false, true),
		)

		It("should break early when both types are found", func() {
			pfDevices := createMockPciDevices([]string{"0009:00:00.0"})
			vfDevices := createMockVfPciDevices([]string{"000a:00:00.1"})
			mixedDevices := append(pfDevices, vfDevices...)
			morePfDevices := createMockPciDevices([]string{"000b:00:00.0", "000c:00:00.0"})
			allDevices := append(mixedDevices, morePfDevices...)

			isPf, isVf := manager.ExportContainsPfOrVfDevices(allDevices)
			Expect(isPf).To(BeTrue())
			Expect(isVf).To(BeTrue())
		})
	})

	Context("extractPCIAddr helper function", func() {
		DescribeTable("should extract PCI address from resource names",
			func(resourceName string, expectedAddr string) {
				addr := manager.ExportExtractPCIAddr(resourceName)
				Expect(addr).To(Equal(expectedAddr))
			},
			Entry("PF resource with address", "spyre_pf_0000_29_00.0", "0000:29:00.0"),
			Entry("VF resource with address", "spyre_vf_0000_ba_00.0", "0000:ba:00.0"),
			Entry("PF resource with complex address", "spyre_pf_0001_3f_10.1", "0001:3f:10.1"),
			Entry("VF resource with complex address", "spyre_vf_0002_4a_05.2", "0002:4a:05.2"),
			Entry("non-per-device PF resource", "spyre_pf", ""),
			Entry("non-per-device VF resource", "spyre_vf", ""),
			Entry("tier resource", "spyre_pf_tier0", "tier0"),
			Entry("empty string", "", ""),
			Entry("random string", "random_resource", ""),
		)
	})

	Context("filterDevicesByPCIAddr helper function", func() {
		It("should filter PF devices by exact device address match", func() {
			// Create mock PF devices with known addresses
			addresses := []string{"0000:29:00.0", "0000:ba:00.0", "0001:3f:10.1"}
			devices := createMockPciDevices(addresses) // These are PF devices

			// Filter for specific PF address
			filtered := manager.ExportFilterDevicesByPCIAddr(devices, "0000:ba:00.0")
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].GetPciAddr()).To(Equal("0000:ba:00.0"))
		})

		It("should filter regular VF devices by PF address match", func() {
			// Create mock VF devices that belong to a specific PF
			vfAddresses := []string{"0000:ba:00.1", "0000:ba:00.2", "0000:29:00.1"}
			devices := createMockVfPciDevices(vfAddresses) // VFs under PF 0000:ba:00.0

			// Filter for PF address should return VFs belonging to that PF
			filtered := manager.ExportFilterDevicesByPCIAddr(devices, "0000:ba:00.0")
			Expect(filtered).To(HaveLen(2)) // Only the first two VFs belong to this PF
			for _, dev := range filtered {
				Expect(dev.GetPfPciAddr()).To(Equal("0000:ba:00.0"))
			}
		})

		It("should filter isolated VF devices by exact device address match", func() {
			// Create mock isolated VF devices (no PF parent)
			isolatedVfAddresses := []string{"0001:00:00.0", "0002:00:00.0", "0003:00:00.0"}
			devices := createMockVfPciDevices(isolatedVfAddresses)

			// Filter for specific isolated VF address
			filtered := manager.ExportFilterDevicesByPCIAddr(devices, "0002:00:00.0")
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].GetPciAddr()).To(Equal("0002:00:00.0"))
			Expect(filtered[0].IsIsolatedVF()).To(BeTrue())
		})

		It("should return empty slice when no match found", func() {
			addresses := []string{"0000:29:00.0", "0000:ba:00.0"}
			devices := createMockPciDevices(addresses)

			filtered := manager.ExportFilterDevicesByPCIAddr(devices, "0001:3f:10.1")
			Expect(filtered).To(HaveLen(0))
		})

		It("should handle mixed device types correctly", func() {
			pfDevices := createMockPciDevices([]string{"0000:ba:00.0"})
			vfDevices := createMockVfPciDevices([]string{"0000:ba:00.1", "0000:ba:00.2"})

			allDevices := append(pfDevices, vfDevices...)

			filtered := manager.ExportFilterDevicesByPCIAddr(allDevices, "0000:ba:00.0")
			Expect(filtered).To(HaveLen(3)) // 1 PF + 2 VFs

			// Check that we got the PF device
			pfCount := 0
			vfCount := 0
			for _, dev := range filtered {
				if dev.IsSriovPF() {
					pfCount++
					Expect(dev.GetPciAddr()).To(Equal("0000:ba:00.0"))
				} else if !dev.IsIsolatedVF() {
					vfCount++
					Expect(dev.GetPfPciAddr()).To(Equal("0000:ba:00.0"))
				}
			}
			Expect(pfCount).To(Equal(1))
			Expect(vfCount).To(Equal(2))
		})

		It("should handle empty device list", func() {
			var devices []types.PciDevice
			filtered := manager.ExportFilterDevicesByPCIAddr(devices, "0000:29:00.0")
			Expect(filtered).To(HaveLen(0))
		})

		It("should handle empty address string", func() {
			addresses := []string{"0000:29:00.0", "0000:ba:00.0"}
			devices := createMockPciDevices(addresses)

			filtered := manager.ExportFilterDevicesByPCIAddr(devices, "")
			Expect(filtered).To(HaveLen(0))
		})
	})
})

func initManagerAndGetResourceServers(configFileName string) []types.ResourceServer {
	// Reset global topology state to ensure clean test
	spyretopo.PciTopology = nil
	if utils.PathExists(spyretopo.DynamicTopologyFilepath) {
		os.Remove(spyretopo.DynamicTopologyFilepath)
	}

	cp := &manager.CliParams{
		ConfigFile:     filepath.Join("..", "..", "..", "test", "data", configFileName),
		ResourcePrefix: "ibm.com",
		ProbePort:      "0", // port will chosen by OS
	}
	// Use the test config from the suite setup
	rm := manager.ExportNewResourceManagerWithConfig(cp, nil, testCfg)
	Expect(rm).NotTo(BeNil())
	defer rm.StopProbeManager()
	err := rm.ReadConfig()
	Expect(err).To(BeNil())
	result := rm.ValidConfigs()
	Expect(result).To(BeTrue())
	err = rm.DiscoverHostDevices()
	Expect(err).To(BeNil())
	err = rm.InitServers()
	Expect(err).To(BeNil())
	rsList := rm.ExportResourceServers()
	return rsList
}

func createMockPciDevices(addresses []string) []types.PciDevice {
	devices := make([]types.PciDevice, 0, len(addresses))
	for _, addr := range addresses {
		pseudoDevice := spyredevice.GeneratePseudoDevice(addr, resources.PfProductId)
		pciDevice := spyredevice.NewPseudoPciDevice(pseudoDevice)
		devices = append(devices, pciDevice)
	}
	return devices
}

func createMockVfPciDevices(vfAddresses []string) []types.PciDevice {
	devices := make([]types.PciDevice, 0, len(vfAddresses))
	for _, vfAddr := range vfAddresses {
		pseudoDevice := spyredevice.GeneratePseudoDevice(vfAddr, resources.VfProductId)
		pciDevice := spyredevice.NewPseudoPciDevice(pseudoDevice)
		devices = append(devices, pciDevice)
	}
	return devices
}
