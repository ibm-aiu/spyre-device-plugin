/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"fmt"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/jaypipes/ghw"
)

// GetAllPCIDevices returns a list of all PCI devices bound to vfio-pci driver
func GetAllPCIDevices() ([]*ghw.PCIDevice, error) {
	pci, err := ghw.PCI(ghw.WithDisableWarnings())
	if err != nil {
		glog.Errorf("Error getting PCI info: %v", err)
		return nil, err
	}

	allDevices := make(map[string]*ghw.PCIDevice)
	for _, device := range pci.Devices {
		if device != nil {
			allDevices[device.Address] = device
		}
	}

	devices := make([]*ghw.PCIDevice, 0)
	pciDevicesPath := getPCIDevicesPath()
	entries, err := os.ReadDir(pciDevicesPath)
	if err != nil {
		glog.Warningf("Failed to read PCI devices directory: %v. Using all ", err)
		for _, device := range allDevices {
			devices = append(devices, device)
		}
		return devices, nil
	}

	for _, entry := range entries {
		name := entry.Name()
		driverLink := fmt.Sprintf("%s/%s/driver", pciDevicesPath, name)
		if target, err := os.Readlink(driverLink); err == nil && strings.Contains(target, "vfio-pci") {
			if device, exists := allDevices[name]; exists {
				devices = append(devices, device)
			}
		}
	}

	return devices, nil
}
