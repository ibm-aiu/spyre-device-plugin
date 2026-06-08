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
	"sync"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	"github.com/jaypipes/ghw"
	"github.com/jaypipes/pcidb"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type PseudoPciDevice struct {
	PciAddress string
	ProductID  string
	pfAddr     string
	apiDevice  *pluginapi.Device

	mu sync.RWMutex
}

func NewPseudoPciDevice(dev *ghw.PCIDevice) types.PciDevice {
	pfAddr := utils.GetPseudoPfAddress(dev.Address)
	apiDevice := &pluginapi.Device{
		ID:       dev.Address,
		Health:   "Healthy",
		Topology: nil,
	}
	return &PseudoPciDevice{
		PciAddress: dev.Address,
		ProductID:  dev.Product.ID,
		pfAddr:     pfAddr,
		apiDevice:  apiDevice,
	}
}

// PciDevice interface
func (d *PseudoPciDevice) GetPciAddr() string {
	if d.PciAddress == "" {
		d.PciAddress = "0000:0000:00:00"
	}
	return d.PciAddress
}

func (d *PseudoPciDevice) GetAPIDevice() *pluginapi.Device {
	return d.apiDevice
}

func (d *PseudoPciDevice) SetHealth(health string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.apiDevice.Health = health
}

func (d *PseudoPciDevice) GetHealth() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.apiDevice.Health
}

func (d *PseudoPciDevice) GetVendor() string {
	return "1014"
}

func (d *PseudoPciDevice) GetDriver() string {
	return "vfio-pci"
}

func (d *PseudoPciDevice) GetDeviceCode() string {
	return d.ProductID
}

func (d *PseudoPciDevice) GetPfPciAddr() string {
	return d.pfAddr
}

func (d *PseudoPciDevice) IsSriovPF() bool {
	return d.ProductID == resources.PfProductId
}

func (d *PseudoPciDevice) GetDeviceSpecs() []*pluginapi.DeviceSpec {
	v := []*pluginapi.DeviceSpec{
		{
			ContainerPath: "/dev/vfio/vfio",
			HostPath:      "/dev/vfio/vfio",
			Permissions:   "mrw",
		},
	}
	return v
}

func (d *PseudoPciDevice) GetEnvVal() string {
	return d.PciAddress
}

func (d *PseudoPciDevice) GetMounts() []*pluginapi.Mount {
	var v []*pluginapi.Mount
	return v
}

func (d *PseudoPciDevice) GetNumaInfo() string {
	return "0"
}

// Isolated VF
func (d *PseudoPciDevice) IsIsolatedVF() bool {
	return d.pfAddr == "" && d.ProductID == resources.VfProductId
}

func GeneratePseudoDevice(address string, productId string) *ghw.PCIDevice {
	pId := productId
	if len(pId) == 0 {
		pId = resources.PfProductId // default
	}
	return &ghw.PCIDevice{
		Address: address,
		Vendor:  &pcidb.Vendor{ID: "1014", Name: "IBM"},
		Product: &pcidb.Product{ID: pId, Name: "unknown"},
		Class:   &pcidb.Class{ID: "00"},
		Driver:  "pseudoDriver",
	}
}
