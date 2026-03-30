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
	"github.com/jaypipes/ghw"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
)

const (
	DeviceEnvKey = "PCIDEVICE_IBM_COM_AIU_PF"
)

// NewPciDevice returns an instance of PciDevice interface
func NewPciDevice(dev *ghw.PCIDevice, rFactory types.ResourceFactory,
	rc *types.ResourceConfig) (types.PciDevice, error) {

	infoProviders := make([]types.DeviceInfoProvider, 0)

	driverName, err := utils.GetDriverName(dev.Address)
	if err != nil {
		return nil, err
	}

	infoProviders = append(infoProviders, rFactory.GetDefaultInfoProvider(dev.Address, driverName))

	pciDev, err := resources.NewPciDevice(dev, rFactory, infoProviders)
	if err != nil {
		return nil, err
	}

	return pciDev, nil
}
