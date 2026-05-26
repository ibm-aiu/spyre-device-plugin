/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jaypipes/ghw"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/health"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
)

var NodeNameEnvKey = utils.NodeNameEnvKey
var healthyDeviceState = *NewDeviceHealthState(pb.DEVICE_TYPE_PF)
var unhealthyDeviceState = *NewUnhealthyDeviceState(pb.DEVICE_TYPE_PF, pb.DEVICE_STATE_IN_ERROR)

var originalPCIDevicesPath string
var testNodeName = "test-node"

func createTestPciDevice(pciAddr string, productID string) types.PciDevice {
	device := spyredevice.GeneratePseudoDevice(pciAddr, productID)
	return spyredevice.NewPseudoPciDevice(device)
}

func createUnhealthtyTestPciDevice(pciAddr string, productID string) types.PciDevice {
	device := spyredevice.GeneratePseudoDevice(pciAddr, productID)
	pciDevice := spyredevice.NewPseudoPciDevice(device)
	pciDevice.SetHealth(pluginapi.Unhealthy)
	return pciDevice
}

func init() {
	os.Setenv(NodeNameEnvKey, testNodeName)
	originalPCIDevicesPath = PCIDevicesPath
}

var _ = Describe("PCI Device Utilities", func() {
	var tempDir string
	var originalPCIDevicesPath string
	var devicePaths []string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "pci-test-*")
		Expect(err).NotTo(HaveOccurred())
		SetPCIDevicesPath(tempDir)
		devicePaths = []string{}
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
		SetPCIDevicesPath(originalPCIDevicesPath)
	})

	createDevicePath := func(addr string) {
		devPath := filepath.Join(tempDir, addr)
		err := os.MkdirAll(devPath, 0755)
		Expect(err).NotTo(HaveOccurred())
		driverPath := filepath.Join(devPath, "driver")
		err = os.Symlink(tempDir, driverPath)
		Expect(err).NotTo(HaveOccurred())

		devicePaths = append(devicePaths, devPath)
	}

	Describe("Health handler basic functionality", func() {
		var handler *HealthInfoHandler

		BeforeEach(func() {
			handler = NewTestHealthInfoHandler()
		})

		It("should handle RediscoverDevices with no devices", func() {
			if runtime.GOOS != "linux" {
				Skip("This test requires Linux")
			}
			createDevicePath("0001:00:00.0")
			err := handler.RediscoverDevices()
			Expect(err).NotTo(HaveOccurred())
			Expect(handler.GetDiscoveredDevices()).NotTo(BeNil())
		})

		It("should handle RediscoverDevices with empty directory", func() {
			if runtime.GOOS != "linux" {
				Skip("This test requires Linux")
			}
			err := handler.RediscoverDevices()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(handler.GetDiscoveredDevices())).To(Equal(0))
		})
	})

	Describe("Health handler logic", func() {
		It("should trigger update correctly", func() {
			handler := NewTestHealthInfoHandler()
			SafeTriggerUpdate(handler.UpdateChan())
			Expect(len(handler.UpdateChan())).To(Equal(1))
			SafeTriggerUpdate(handler.UpdateChan())
			Expect(len(handler.UpdateChan())).To(Equal(1))
		})

		It("should be a single thread and able to debounce", func() {
			handler := NewTestHealthInfoHandler()
			handler.StartTestMode()
			start := time.Now()
			SafeTriggerUpdate(handler.UpdateChan())
			time.Sleep(2 * time.Second)
			SafeTriggerUpdate(handler.UpdateChan()) // this request should be debounced 5s
			time.Sleep(10 * time.Second)
			timeToCompleteFirstRequest := handler.GetLastUpdate().Sub(start).Seconds()
			Expect(timeToCompleteFirstRequest).To(BeNumerically(">=", 10))
			Expect(timeToCompleteFirstRequest).To(BeNumerically("<", 12)) // allow 2s safe-zone padding
			time.Sleep(17 * time.Second)                                  // process + debounce time
			timeToCompleteSecondRequest := handler.GetLastUpdate().Sub(start).Seconds()
			Expect(timeToCompleteSecondRequest).To(BeNumerically(">=", 25))
			Expect(timeToCompleteSecondRequest).To(BeNumerically("<", 27)) // allow 2s safe-zone padding
			close(handler.StopChan())
			close(handler.UpdateChan())
		})

		DescribeTable("InitHealthInfo", func(allDevices []types.PciDevice, expectedIsSriovPF bool) {
			healthInfoMap := InitHealthInfo(allDevices)
			Expect(healthInfoMap).To(HaveLen(len(allDevices)))
			for _, device := range allDevices {
				addr := device.GetPciAddr()
				healthInfo, found := healthInfoMap[addr]
				Expect(found).To(BeTrue())
				Expect(healthInfo.Healthy()).To(BeTrue())
				Expect(healthInfo.IsSriovPF()).To(Equal(expectedIsSriovPF))
			}
		},
			Entry("empty list", []types.PciDevice{}, false),
			Entry("pf device list", []types.PciDevice{
				createTestPciDevice("0001:00:00.0", resources.PfProductId),
				createTestPciDevice("0002:00:00.0", resources.PfProductId),
			}, true),
			Entry("vf device list", []types.PciDevice{
				createTestPciDevice("0001:00:00.1", resources.VfProductId),
				createTestPciDevice("0002:00:00.1", resources.VfProductId),
			}, false),
		)
	})

	Describe("Device info helpers", func() {
		It("should get device info correctly", func() {
			if runtime.GOOS != "linux" {
				Skip("This test requires Linux")
			}
			handler := NewTestHealthInfoHandler()
			allDevices, err := handler.RediscoverAndGetDeviceInfo(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(allDevices).NotTo(BeNil())
		})
	})

	Describe("HealthInfoHandler creation", func() {
		var deviceProvider types.DeviceProvider
		var spyreClient *spyreclient.SpyreClient
		var err error

		BeforeEach(func() {
			spyreClient, err = spyreclient.NewClient(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred(), "Failed to create SpyreClient")
			Expect(spyreClient).NotTo(BeNil(), "SpyreClient is nil after creation")

			deviceProvider = NewTestDeviceProvider()
		})

		It("should create a new PCIMonitor and HealthInfoHandler with default intervals", func() {
			checker := NewPCIMonitor(0)
			Expect(checker.ScanInterval).To(Equal(DefaultScanInterval))
			handler, err := NewHealthInfoHandler(deviceProvider, checker, cfg, spyreClient, 0, nil)
			Expect(handler).NotTo(BeNil())
			Expect(err).To(BeNil())
			Expect(handler.GetDebounceInterval()).To(Equal(DefaultDebounceInterval))
		})

		It("should create a new PCIMonitor and HealthInfoHandler with custom intervals", func() {
			customScanInterval := 10 * time.Second
			customDebounceInterval := 3 * time.Second
			checker := NewPCIMonitor(customScanInterval)
			Expect(checker.ScanInterval).To(Equal(customScanInterval))
			handler, err := NewHealthInfoHandler(deviceProvider, checker, cfg, spyreClient, customDebounceInterval, nil)
			Expect(handler).NotTo(BeNil())
			Expect(err).To(BeNil())
			Expect(handler.GetDebounceInterval()).To(Equal(customDebounceInterval))
		})

		It("cannot create if any of deviceProvider, spyreClient, and cfg is nil", func() {
			checker := NewPCIMonitor(0)
			_, err := NewHealthInfoHandler(nil, checker, nil, nil, 0, nil)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("DeviceProvider"))
			_, err = NewHealthInfoHandler(deviceProvider, checker, nil, nil, 0, nil)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("rest.Config"))
			_, err = NewHealthInfoHandler(deviceProvider, checker, cfg, nil, 0, nil)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("spyreClient"))
		})
	})

	Describe("HealthInfoHandler lifecycle", func() {
		var handler *HealthInfoHandler
		var deviceProvider types.DeviceProvider
		var ctx context.Context
		var cancel context.CancelFunc
		var spyreClient *spyreclient.SpyreClient

		BeforeEach(func() {
			var err error
			ctx, cancel = context.WithCancel(context.Background())
			spyreClient, err = spyreclient.NewClient(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred(), "Failed to create SpyreClient")
			Expect(spyreClient).NotTo(BeNil(), "SpyreClient is nil after creation")

			deviceProvider = NewTestDeviceProvider()
			checker := NewPCIMonitor(100 * time.Millisecond)
			handler, err = NewHealthInfoHandler(deviceProvider, checker, cfg, spyreClient, 50*time.Millisecond, nil)
			Expect(err).NotTo(HaveOccurred(), "Failed to create handler")
			Expect(handler).NotTo(BeNil(), "Handler is nil after creation")
		})

		AfterEach(func() {
			cancel()
		})

		It("should start and stop monitoring correctly", func() {
			if runtime.GOOS != "linux" {
				Skip("This test requires Linux")
			}
			handler.Start(ctx, []types.PciDevice{})
			time.Sleep(200 * time.Millisecond)
			handler.Stop()
			_, ok := <-handler.StopChan()
			Expect(ok).To(BeFalse())
		})
	})
})

var _ = Describe("DeviceProvider AddTargetDevices", func() {
	var deviceProvider types.DeviceProvider

	BeforeEach(func() {
		deviceProvider = NewTestDeviceProvider()
	})

	Describe("AddTargetDevices accumulation behavior", func() {
		It("should accumulate devices correctly when adding overlapping sets", func() {

			initialDevices := []*ghw.PCIDevice{
				spyredevice.GeneratePseudoDevice("0000:01:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:02:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:03:00.0", resources.PfProductId),
			}
			err := deviceProvider.AddTargetDevices(initialDevices, []int64{0x00})
			Expect(err).NotTo(HaveOccurred())
			discoveredDevices := deviceProvider.GetDiscoveredDevices()
			Expect(discoveredDevices).To(HaveLen(3))
			newDevices := []*ghw.PCIDevice{
				spyredevice.GeneratePseudoDevice("0000:01:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:02:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:04:00.0", resources.PfProductId),
			}
			err = deviceProvider.AddTargetDevices(newDevices, []int64{0x00})
			Expect(err).NotTo(HaveOccurred())
			finalDevices := deviceProvider.GetDiscoveredDevices()
			Expect(finalDevices).To(HaveLen(4))
			addresses := make([]string, len(finalDevices))
			for i, dev := range finalDevices {
				addresses[i] = dev.Address
			}
			Expect(addresses).To(ContainElements(
				"0000:01:00.0",
				"0000:02:00.0",
				"0000:03:00.0",
				"0000:04:00.0",
			))
		})

		It("should handle completely new device sets", func() {
			initialDevices := []*ghw.PCIDevice{
				spyredevice.GeneratePseudoDevice("0000:01:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:02:00.0", resources.PfProductId),
			}
			err := deviceProvider.AddTargetDevices(initialDevices, []int64{0x00})
			Expect(err).NotTo(HaveOccurred())
			newDevices := []*ghw.PCIDevice{
				spyredevice.GeneratePseudoDevice("0000:03:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:04:00.0", resources.PfProductId),
			}
			err = deviceProvider.AddTargetDevices(newDevices, []int64{0x00})
			Expect(err).NotTo(HaveOccurred())
			finalDevices := deviceProvider.GetDiscoveredDevices()
			Expect(finalDevices).To(HaveLen(4))
			addresses := make([]string, len(finalDevices))
			for i, dev := range finalDevices {
				addresses[i] = dev.Address
			}
			Expect(addresses).To(ContainElements(
				"0000:01:00.0",
				"0000:02:00.0",
				"0000:03:00.0",
				"0000:04:00.0",
			))
		})
	})
})

var _ = Describe("Device type identification", func() {
	Describe("containsVForPF", func() {
		It("should identify PF devices correctly", func() {
			pfDevices := []types.PciDevice{
				createTestPciDevice("0001:00:00.0", resources.PfProductId),
				createTestPciDevice("0002:00:00.0", resources.PfProductId),
			}

			isPf, isVf := ContainsVForPF(pfDevices)

			Expect(isPf).To(BeTrue())
			Expect(isVf).To(BeFalse())
		})

		It("should identify VF devices correctly", func() {
			vfDevices := []types.PciDevice{
				createTestPciDevice("0001:00:00.1", resources.VfProductId),
				createTestPciDevice("0002:00:00.1", resources.VfProductId),
			}

			isPf, isVf := ContainsVForPF(vfDevices)

			Expect(isPf).To(BeFalse())
			Expect(isVf).To(BeTrue())
		})

		It("should identify mixed PF and VF devices", func() {
			mixedDevices := []types.PciDevice{
				createTestPciDevice("0001:00:00.0", resources.PfProductId),
				createTestPciDevice("0001:00:00.1", resources.VfProductId),
			}

			isPf, isVf := ContainsVForPF(mixedDevices)

			Expect(isPf).To(BeTrue())
			Expect(isVf).To(BeTrue())
		})

		It("should handle empty device list", func() {
			emptyDevices := []types.PciDevice{}

			isPf, isVf := ContainsVForPF(emptyDevices)

			Expect(isPf).To(BeFalse())
			Expect(isVf).To(BeFalse())
		})

		It("should handle single PF device", func() {
			singlePF := []types.PciDevice{
				createTestPciDevice("0001:00:00.0", resources.PfProductId),
			}

			isPf, isVf := ContainsVForPF(singlePF)

			Expect(isPf).To(BeTrue())
			Expect(isVf).To(BeFalse())
		})

		It("should handle single VF device", func() {
			singleVF := []types.PciDevice{
				createTestPciDevice("0001:00:00.1", resources.VfProductId),
			}

			isPf, isVf := ContainsVForPF(singleVF)

			Expect(isPf).To(BeFalse())
			Expect(isVf).To(BeTrue())
		})

		It("should break early when both PF and VF devices are found", func() {
			mixedDevicesEarlyBreak := []types.PciDevice{
				createTestPciDevice("0001:00:00.0", resources.PfProductId),
				createTestPciDevice("0001:00:00.1", resources.VfProductId),
				createTestPciDevice("0002:00:00.0", resources.PfProductId),
				createTestPciDevice("0002:00:00.1", resources.VfProductId),
			}

			isPf, isVf := ContainsVForPF(mixedDevicesEarlyBreak)

			Expect(isPf).To(BeTrue())
			Expect(isVf).To(BeTrue())
		})
	})

	Describe("Device Health Monitoring and Topology Updates", func() {
		var handler *HealthInfoHandler

		BeforeEach(func() {
			handler = NewTestHealthInfoHandler()
		})

		DescribeTable("ProcessDeviceHealth", func(devices []types.PciDevice, uniqueDeviceNum int,
			healthInfoMap map[string]DeviceHealthState, expectedHealthy map[string]bool) {
			uniqueDevices, unhealthyDevices := handler.ProcessDeviceHealth(devices, healthInfoMap)
			if expectedHealthy == nil {
				Expect(uniqueDevices).To(BeEmpty())
				return
			}
			Expect(uniqueDevices).To(HaveLen(len(expectedHealthy)), fmt.Sprintf("expect %d results, got %v", len(expectedHealthy), uniqueDevices))
			healthyNum := 0
			for addr, device := range uniqueDevices {
				expected, found := expectedHealthy[addr]
				Expect(found).To(BeTrue(), "%s is unexpected", addr)
				healthyResult := device.GetHealth() == pluginapi.Healthy
				if healthyResult {
					healthyNum += 1
				}
				Expect(healthyResult).To(Equal(expected), "unexpected healthiness")
			}
			expectedUnhealthy := uniqueDeviceNum - healthyNum
			Expect(len(unhealthyDevices)).To(Equal(expectedUnhealthy))
			processedUnhealthy := make(map[string]bool, len(unhealthyDevices))
			for _, dev := range unhealthyDevices {
				healthy, found := expectedHealthy[dev.ID]
				Expect(found && healthy).To(BeFalse(), "%s must not be in unhealthy list", dev.ID)
				Expect(dev.State).NotTo(BeEquivalentTo(pb.DEVICE_STATE_ONLINE.String()))
				Expect(processedUnhealthy[dev.ID]).To(BeFalse(), "unhealthy list must be unique")
				processedUnhealthy[dev.ID] = true
			}
		},
			Entry("can handle empty device list", []types.PciDevice{}, 0, map[string]DeviceHealthState{}, nil),
			Entry("can identify all healthy devices when they exist in filesystem",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				}, 2,
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				},
			),
			Entry("can identify vf health",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.1", resources.VfProductId),
					createUnhealthtyTestPciDevice("0001:00:00.2", resources.VfProductId),
				}, 2,
				map[string]DeviceHealthState{
					"0001:00:00.1": healthyDeviceState,
					"0001:00:00.2": unhealthyDeviceState,
				},
				map[string]bool{
					"0001:00:00.1": true,
					"0001:00:00.2": false,
				},
			),
			Entry("can process devices with different filesystem states",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				}, 2,
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					// 0002:00:00.0 missing health info
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				},
			),
			Entry("can process change healthy from unhealthy state and vice versa",
				[]types.PciDevice{
					createUnhealthtyTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				}, 2,
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": unhealthyDeviceState,
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": false,
				},
			),
			Entry("can process device list with duplicated items",
				[]types.PciDevice{
					createUnhealthtyTestPciDevice("0001:00:00.0", resources.PfProductId),
					createUnhealthtyTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				}, 2,
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": unhealthyDeviceState,
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": false,
				},
			),
			Entry("can inherit unhealthy from PF",
				[]types.PciDevice{
					createUnhealthtyTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0001:00:00.1", resources.VfProductId),
				}, 2,
				map[string]DeviceHealthState{
					"0001:00:00.0": unhealthyDeviceState,
					"0001:00:00.1": healthyDeviceState,
				},
				map[string]bool{
					"0001:00:00.0": false,
					"0001:00:00.1": false,
				},
			),
		)

		DescribeTable("IdentifyDeviceChanges", func(devices []types.PciDevice, existing map[string]bool,
			healthInfoMap map[string]DeviceHealthState, expectedNewDevices []string, expectedChange bool) {
			newDevices, changed := handler.IdentifyDeviceChanges(devices, existing, healthInfoMap)
			Expect(changed).To(Equal(expectedChange))
			Expect(newDevices).To(HaveLen(len(expectedNewDevices)), fmt.Sprintf("expect %d results, got %v", len(expectedNewDevices), newDevices))
			for _, device := range newDevices {
				Expect(expectedNewDevices).To(ContainElement(device.GetPciAddr()))
			}
		},
			Entry("can detect when no device changes occur",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
				},
				[]string{}, false,
			),
			Entry("can detect when a device is removed",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
				}, map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				}, map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
				}, []string{}, true,
			),
			Entry("can detect multiple device removals",
				[]types.PciDevice{},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
					"0003:00:00.0": true,
				}, map[string]DeviceHealthState{}, []string{}, true,
			),
			Entry("can detect when a new device is added",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				},
				map[string]bool{
					"0001:00:00.0": true,
				},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
				},
				[]string{"0002:00:00.0"}, true,
			),
			Entry("can detect multiple device additions",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
					createTestPciDevice("0003:00:00.0", resources.PfProductId),
				},
				map[string]bool{},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
					"0003:00:00.0": healthyDeviceState,
				},
				[]string{"0001:00:00.0", "0002:00:00.0", "0003:00:00.0"}, true,
			),
			Entry("can handle simultaneous additions and removals",
				[]types.PciDevice{
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
					createTestPciDevice("0003:00:00.0", resources.PfProductId),
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				},
				map[string]DeviceHealthState{
					"0002:00:00.0": healthyDeviceState,
					"0003:00:00.0": healthyDeviceState,
				},
				[]string{"0003:00:00.0"}, true,
			),
			Entry("can detect present device change from healthy to unhealthy state",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": unhealthyDeviceState,
				},
				[]string{}, true,
			),
			Entry("can detect present device change from unhealthy to healthy state",
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createUnhealthtyTestPciDevice("0002:00:00.0", resources.PfProductId),
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
				},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
				},
				[]string{}, true,
			),
			Entry("can detect simultaneous additions, removals, healthy changes",
				[]types.PciDevice{
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
					createTestPciDevice("0003:00:00.0", resources.PfProductId),
					createTestPciDevice("0004:00:00.0", resources.PfProductId),
					createUnhealthtyTestPciDevice("0005:00:00.0", resources.PfProductId),
				},
				map[string]bool{
					"0001:00:00.0": true,
					"0002:00:00.0": true,
					"0004:00:00.0": true,
					"0005:00:00.0": true,
				},
				map[string]DeviceHealthState{
					"0002:00:00.0": healthyDeviceState,
					"0003:00:00.0": healthyDeviceState,
					"0004:00:00.0": unhealthyDeviceState,
					"0005:00:00.0": healthyDeviceState,
				},
				[]string{"0003:00:00.0"}, true,
			),
		)
	})
})
