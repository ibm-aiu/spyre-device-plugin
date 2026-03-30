/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"context"
	"sync"
	"time"

	"github.com/golang/glog"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	"github.com/jaypipes/ghw"
)

const (
	DefaultDebounceInterval = 5 * time.Second
	DefaultScanInterval     = 30 * time.Second
)

var (
	PCIDevicesPath   = "/sys/bus/pci/devices/"
	pciDevicesPathMu sync.RWMutex
)

// getPCIDevicesPath returns the current PCI devices path in a thread-safe manner
func getPCIDevicesPath() string {
	pciDevicesPathMu.RLock()
	defer pciDevicesPathMu.RUnlock()
	return PCIDevicesPath
}

// setPCIDevicesPath sets the PCI devices path in a thread-safe manner
func setPCIDevicesPath(path string) {
	pciDevicesPathMu.Lock()
	defer pciDevicesPathMu.Unlock()
	PCIDevicesPath = path
}

// PCIMonitor implements HealthChecker.
// It monitors PCI devices with given scan interval,
// and trigger updates if changes detected.
type PCIMonitor struct {
	ScanInterval time.Duration
	StopChan     chan struct{}
}

func NewPCIMonitor(scanInterval time.Duration) *PCIMonitor {
	if scanInterval <= 0 {
		scanInterval = DefaultScanInterval
	}
	return &PCIMonitor{
		ScanInterval: scanInterval,
		StopChan:     make(chan struct{}),
	}
}

func (m *PCIMonitor) Start(ctx context.Context, updateChan chan struct{}, intialDevices *pb.Devices) error {
	go m.scanLoop(ctx, updateChan)
	return nil
}

func (m *PCIMonitor) Stop() {
	close(m.StopChan)
	glog.Info("PCI monitor stopped")
}

// UpdateHealths updates nothing (no state report)
func (m *PCIMonitor) UpdateHealths(map[string]DeviceHealthState) {}

// periodically scans PCI devices
func (m *PCIMonitor) scanLoop(ctx context.Context, updateChan chan struct{}) {
	ticker := time.NewTicker(m.ScanInterval)
	defer ticker.Stop()

	devices, err := GetAllPCIDevices()
	if err != nil {
		glog.Errorf("Failed to scan initial PCI devices: %v", err)
		devices = []*ghw.PCIDevice{}
	}

	prevDevices := make(map[string]any)
	for _, dev := range devices {
		prevDevices[dev.Address] = struct{}{}
	}

	if len(devices) > 0 {
		glog.Infof("Initial device inventory contains %d devices:", len(devices))
		for _, dev := range devices {
			glog.V(1).Infof("   - %s: present at startup", dev.Address)
		}
	}

	SafeTriggerUpdate(updateChan)

	for {
		select {
		case <-ticker.C:
			currentDevices, err := GetAllPCIDevices()
			if err != nil {
				glog.Errorf("Failed to scan PCI devices: %v", err)
				continue
			}

			currentDeviceMap := make(map[string]any)
			for _, dev := range currentDevices {
				currentDeviceMap[dev.Address] = struct{}{}
			}

			if !MapsEqual(prevDevices, currentDeviceMap) {
				for addr := range prevDevices {
					if _, exists := currentDeviceMap[addr]; !exists {
						glog.Infof("Device %s removed", addr)
					}
				}
				for addr := range currentDeviceMap {
					if _, exists := prevDevices[addr]; !exists {
						glog.Infof("Device %s added", addr)
					}
				}
				prevDevices = currentDeviceMap
				SafeTriggerUpdate(updateChan)
			}

		case <-m.StopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}
