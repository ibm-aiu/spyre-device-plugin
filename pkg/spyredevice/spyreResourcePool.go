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
	"github.com/golang/glog"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
)

type spyreResourcePool struct {
	*resources.ResourcePoolImpl
	selectors *types.DeviceSelectors
}

var _ types.ResourcePool = &spyreResourcePool{}

// NewSpyreResourcePool returns an instance of resourcePool
func NewSpyreResourcePool(rc *types.ResourceConfig, apiDevices map[string]*pluginapi.Device,
	devicePool map[string]types.PciDevice) types.ResourcePool {

	glog.V(1).Info("creating new resource pool")
	rp := resources.NewResourcePool(rc, apiDevices, devicePool)
	s, _ := rc.SelectorObj.(*types.DeviceSelectors)

	return &spyreResourcePool{
		ResourcePoolImpl: rp,
		selectors:        s,
	}
}

// Overrides GetDeviceSpecs
func (rp *spyreResourcePool) GetDeviceSpecs(deviceIDs []string) []*pluginapi.DeviceSpec {
	glog.V(1).Infof("GetDeviceSpecs(): for devices: %v", deviceIDs)
	devSpecs := make([]*pluginapi.DeviceSpec, 0)

	devicePool := rp.GetDevicePool()

	// Add device driver specific
	for _, id := range deviceIDs {
		if dev, ok := devicePool[id]; ok {
			newSpecs := dev.GetDeviceSpecs()
			for _, ds := range newSpecs {
				if !rp.DeviceSpecExist(devSpecs, ds) {
					devSpecs = append(devSpecs, ds)
				}
			}
		}
	}
	return devSpecs
}

func GetResourcePoolImpl(rp types.ResourcePool) *resources.ResourcePoolImpl {
	return rp.(*spyreResourcePool).ResourcePoolImpl
}
