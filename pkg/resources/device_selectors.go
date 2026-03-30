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

package resources

import (
	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
)

// NewVendorSelector returns a DeviceSelector interface for vendor list
func NewVendorSelector(vendors []string) types.DeviceSelector {
	return &vendorSelector{vendors: vendors}
}

type vendorSelector struct {
	vendors []string
}

func (s *vendorSelector) Filter(inDevices []types.PciDevice) []types.PciDevice {
	filteredList := make([]types.PciDevice, 0)
	glog.V(1).Infof("s.devices: %v devCode: %v", s.vendors, inDevices)
	for _, dev := range inDevices {
		devVendor := dev.GetVendor()
		glog.V(1).Infof("s.vendors: %s devVendor: %s", s.vendors, devVendor)
		if contains(s.vendors, devVendor) {
			glog.V(1).Infof("contain s.vendors: %s devVendor: %s", s.vendors, devVendor)
			filteredList = append(filteredList, dev)
		}
	}
	return filteredList
}

// NewDeviceSelector returns a DeviceSelector interface for device list
func NewDeviceSelector(devices []string) types.DeviceSelector {
	return &deviceSelector{devices: devices}
}

type deviceSelector struct {
	devices []string
}

func (s *deviceSelector) Filter(inDevices []types.PciDevice) []types.PciDevice {
	filteredList := make([]types.PciDevice, 0)
	glog.V(1).Infof("s.devices: %v devCode: %v", s.devices, inDevices)
	for _, dev := range inDevices {
		devCode := dev.GetDeviceCode()
		glog.V(1).Infof("s.devices: %s devCode: %s", s.devices, devCode)
		if contains(s.devices, devCode) {
			glog.V(1).Infof("contain s.devices: %s devCode: %s", s.devices, devCode)
			filteredList = append(filteredList, dev)
		}
	}
	return filteredList
}

// NewDriverSelector returns a selector interface for driver list
func NewDriverSelector(drivers []string) types.DeviceSelector {
	return &driverSelector{drivers: drivers}
}

type driverSelector struct {
	drivers []string
}

func (s *driverSelector) Filter(inDevices []types.PciDevice) []types.PciDevice {
	filteredList := make([]types.PciDevice, 0)
	for _, dev := range inDevices {
		devDriver := dev.GetDriver()
		if contains(s.drivers, devDriver) {
			filteredList = append(filteredList, dev)
		}
	}
	return filteredList
}

// NewPciAddressSelector returns a selector interface for pci address list
func NewPciAddressSelector(pciAddresses []string) types.DeviceSelector {
	return &pciAddressSelector{pciAddresses: pciAddresses}
}

type pciAddressSelector struct {
	pciAddresses []string
}

func (s *pciAddressSelector) Filter(inDevices []types.PciDevice) []types.PciDevice {
	filteredList := make([]types.PciDevice, 0)
	for _, dev := range inDevices {
		if contains(s.pciAddresses, dev.GetPciAddr()) {
			filteredList = append(filteredList, dev)
		}
	}
	return filteredList
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
