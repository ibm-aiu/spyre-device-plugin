/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
//
// Copyright 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spyredevice

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/exp/slices"

	"github.com/golang/glog"
	"github.com/jaypipes/ghw"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
)

const (
	maxVendorNameLen  = 20
	maxProductNameLen = 40
	classIDBaseInt    = 16
	classIDBitSize    = 64
)

type spyreDeviceProvider struct {
	deviceList []*ghw.PCIDevice
	rFactory   types.ResourceFactory
}

// NewSpyreDeviceProvider DeviceProvider implementation from spyreDeviceProvider instance
func NewSpyreDeviceProvider(rf types.ResourceFactory) types.DeviceProvider {
	return &spyreDeviceProvider{
		rFactory:   rf,
		deviceList: make([]*ghw.PCIDevice, 0),
	}
}

func (dp *spyreDeviceProvider) GetDiscoveredDevices() []*ghw.PCIDevice {
	return dp.deviceList
}

func (dp *spyreDeviceProvider) GetDevices(rc *types.ResourceConfig) []types.PciDevice {
	newPciDevices := make([]types.PciDevice, 0)
	for _, device := range dp.deviceList {
		glog.V(1).Infof("+++ Device +++ : %v", device)
		if os.Getenv(spyrev1alpha1.PseudoDeviceMode.EnvKey()) == spyreconst.ModeEnabledValue {
			newDevice := NewPseudoPciDevice(device)
			newPciDevices = append(newPciDevices, newDevice)
		} else {
			if newDevice, err := NewPciDevice(device, dp.rFactory, rc); err == nil {
				newPciDevices = append(newPciDevices, newDevice)
			} else {
				glog.Errorf("error creating new device: %q", err)
			}
		}
	}
	return newPciDevices
}

func (dp *spyreDeviceProvider) AddTargetDevices(devices []*ghw.PCIDevice, deviceCodes []int64) error {
	// Create a map to track existing devices by address
	deviceMap := make(map[string]*ghw.PCIDevice)

	// Add existing devices to the map
	for _, device := range dp.deviceList {
		deviceMap[device.Address] = device
	}

	for _, device := range devices {
		if _, exists := deviceMap[device.Address]; exists {
			glog.V(1).Infof("Skipping duplicate device %s", device.Address)
			continue
		}

		devClass, err := strconv.ParseInt(device.Class.ID, classIDBaseInt, classIDBitSize)
		if err != nil {
			glog.Warningf("spyredevice AddTargetDevices(): unable to parse device class for device %+v %q", device, err)
			continue
		}

		// If deviceCodes is empty, accept all devices; otherwise check if device class is in the list
		if len(deviceCodes) == 0 || slices.Contains(deviceCodes, devClass) {
			vendor := device.Vendor
			vendorName := vendor.Name
			if len(vendor.Name) > maxVendorNameLen {
				vendorName = string([]byte(vendorName)[0:17]) + "..."
			}
			product := device.Product
			productName := product.Name
			if len(product.Name) > maxProductNameLen {
				productName = string([]byte(productName)[0:37]) + "..."
			}
			glog.V(1).Infof("spyredevice: device type found. address: %-12s, classID: %-12s, vendor: %s (%-20s), product: %s (%-40s), driver: %s", //nolint:lll
				device.Address, device.Class.ID, vendor.ID, vendorName, product.ID, productName, device.Driver)
			// Add to device map
			deviceMap[device.Address] = device
		}
	}

	// Rebuild the device list from the map to preserve accumulated devices
	newDeviceList := make([]*ghw.PCIDevice, 0, len(deviceMap))
	for _, device := range deviceMap {
		newDeviceList = append(newDeviceList, device)
	}
	dp.deviceList = newDeviceList
	return nil
}

//nolint:gocyclo
func (dp *spyreDeviceProvider) GetFilteredDevices(devices []types.PciDevice,
	rc *types.ResourceConfig) ([]types.PciDevice, error) {

	filteredDevice := devices
	nf, ok := rc.SelectorObj.(*types.DeviceSelectors)
	if !ok {
		return filteredDevice, fmt.Errorf("unable to convert SelectorObj to DeviceSelectors")
	}

	glog.V(1).Infof("number of filteredDevice before filtering by vendors: %d\n", len(filteredDevice))
	rf := dp.rFactory
	// filter by vendor list
	if len(nf.Vendors) > 0 {
		if selector, err := rf.GetSelector("vendors", nf.Vendors); err == nil {
			filteredDevice = selector.Filter(filteredDevice)
		}
	}

	glog.V(1).Infof("number of filteredDevice before filtering by devices: %d\n", len(filteredDevice))
	// filter by device list
	if len(nf.Devices) > 0 {
		if selector, err := rf.GetSelector("devices", nf.Devices); err == nil {
			filteredDevice = selector.Filter(filteredDevice)
		}
	}

	glog.V(1).Infof("number of filteredDevice before filtering by drivers: %d\n", len(filteredDevice))
	// filter by driver list
	if len(nf.Drivers) > 0 {
		if selector, err := rf.GetSelector("drivers", nf.Drivers); err == nil {
			filteredDevice = selector.Filter(filteredDevice)
		}
	}

	glog.V(1).Infof("number of filteredDevice before filtering by PciAddresses: %d\n", len(filteredDevice))
	// filter by pciAddresses list
	if len(nf.PciAddresses) > 0 {
		if selector, err := rf.GetSelector("pciAddresses", nf.PciAddresses); err == nil {
			filteredDevice = selector.Filter(filteredDevice)
		}
	}

	return filteredDevice, nil
}

// ValidConfig performs validation of DeviceSelectors
func (dp *spyreDeviceProvider) ValidConfig(rc *types.ResourceConfig) bool {
	nf, ok := rc.SelectorObj.(*types.DeviceSelectors)
	if !ok {
		glog.Errorf("unable to convert SelectorObj to DeviceSelectors")
		return false
	}
	if nf == nil {
		glog.Errorf("can not get SelectorObj")
		return false
	}
	return true
}
