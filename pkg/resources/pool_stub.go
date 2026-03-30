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
	"sync"

	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// ResourcePoolImpl implements stub ResourcePool interface
type ResourcePoolImpl struct {
	config             *types.ResourceConfig
	devices            map[string]*pluginapi.Device
	devicePool         map[string]types.PciDevice
	spyreConfigHandler *spyreconf.ConfigHandler
	otherAllocation    map[string]bool
	selfAllocation     map[string]bool
	mu                 sync.RWMutex
	topology           *pcitopov2.Pcitopo
	isTopologyAware    bool
}

var _ types.ResourcePool = &ResourcePoolImpl{}

// NewResourcePool returns an instance of resourcePool
func NewResourcePool(rc *types.ResourceConfig, apiDevices map[string]*pluginapi.Device,
	devicePool map[string]types.PciDevice) *ResourcePoolImpl {
	spyreConfigHandler, err := spyreconf.InitConfigMount()
	if err != nil {
		glog.Errorf("error init config mount %v for %s", err, rc.ResourceName)
	}
	var topoInterface *pcitopov2.Pcitopo
	topo, err := spyretopo.GetPciTopology("", false)
	if err == nil {
		topoInterface = &topo
	}
	return &ResourcePoolImpl{
		config:             rc,
		devices:            apiDevices,
		devicePool:         devicePool,
		spyreConfigHandler: spyreConfigHandler,
		otherAllocation:    make(map[string]bool),
		selfAllocation:     make(map[string]bool),
		topology:           topoInterface,
		isTopologyAware:    utils.IsTopologyAwareResource(rc.ResourceName),
	}
}

// GetConfig returns ResourceConfig for this resourcePool
func (rp *ResourcePoolImpl) GetConfig() *types.ResourceConfig {
	return rp.config
}

// InitDevice - not implemented
func (rp *ResourcePoolImpl) InitDevice() error {
	// Not implemented
	return nil
}

// GetResourceName returns the resource name as string
func (rp *ResourcePoolImpl) GetResourceName() string {
	return rp.config.ResourceName
}

// GetResourcePrefix returns the resource name prefix as string
func (rp *ResourcePoolImpl) GetResourcePrefix() string {
	return rp.config.ResourcePrefix
}

// GetDevices returns a map of Kubelet API devices after removing allocated devices
func (rp *ResourcePoolImpl) GetDevices() map[string]*pluginapi.Device {
	rp.mu.Lock()
	// returns all devices from devices[]
	availableDevices := make(map[string]*pluginapi.Device)
	for id, device := range rp.devices {
		if allocated, found := rp.otherAllocation[device.GetID()]; found && allocated {
			continue
		}
		availableDevices[id] = device
	}
	rp.mu.Unlock()
	return availableDevices
}

// Probe - does device healthcheck. Not implemented
func (rp *ResourcePoolImpl) Probe() bool {
	// TO-DO: Implement this
	return false
}

// GetDeviceSpecs returns list of plugin API device specs for a list of device IDs
func (rp *ResourcePoolImpl) GetDeviceSpecs(deviceIDs []string) []*pluginapi.DeviceSpec {
	glog.V(1).Infof("GetDeviceSpecs(): for devices: %v", deviceIDs)
	devSpecs := make([]*pluginapi.DeviceSpec, 0)

	// Add vfio group specific devices
	for _, id := range deviceIDs {
		if dev, ok := rp.devicePool[id]; ok {
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

// GetEnvs returns a list of device specific Env values for device IDs
func (rp *ResourcePoolImpl) GetEnvs(deviceIDs []string) []string {
	glog.V(1).Infof("GetEnvs(): for devices: %v", deviceIDs)
	devEnvs := make([]string, 0)

	// Consolidates all Envs
	for _, id := range deviceIDs {
		if dev, ok := rp.devicePool[id]; ok {
			env := dev.GetEnvVal()
			devEnvs = append(devEnvs, env)
		}
	}

	return devEnvs
}

// GetMounts returns a list of Mount for device IDs
func (rp *ResourcePoolImpl) GetMounts(deviceIDs []string) []*pluginapi.Mount {
	glog.V(1).Infof("GetMounts(): for devices: %v", deviceIDs)
	devMounts := make([]*pluginapi.Mount, 0)

	for _, id := range deviceIDs {
		if dev, ok := rp.devicePool[id]; ok {
			mnt := dev.GetMounts()
			devMounts = append(devMounts, mnt...)
		}
	}
	confMounts, err := rp.spyreConfigHandler.GetConfigMetricsMount(rp.GetResourceName(), deviceIDs)
	if err != nil {
		glog.V(1).Infof("failed to get config mounts: %v", err)
	}
	devMounts = append(devMounts, confMounts...)
	return devMounts
}

// DeviceSpecExist checks if a DeviceSpec already exist in a DeviceSpec list
func (rp *ResourcePoolImpl) DeviceSpecExist(specs []*pluginapi.DeviceSpec, newSpec *pluginapi.DeviceSpec) bool {
	for _, sp := range specs {
		if sp.HostPath == newSpec.HostPath {
			return true
		}
	}
	return false
}

// GetDevicePool returns PciDevice pool as a map
func (rp *ResourcePoolImpl) GetDevicePool() map[string]types.PciDevice {
	return rp.devicePool
}

func (rp *ResourcePoolImpl) InformedBySharedInfo(deviceList []string, allocated bool, self bool) (changed bool) {
	rp.mu.Lock()
	var allocationMap map[string]bool
	if self {
		allocationMap = rp.selfAllocation
	} else {
		allocationMap = rp.otherAllocation
		if !allocated {
			// for anonymous de-allocation
			for _, deviceID := range deviceList {
				// check if self allocated
				if prevAllocated, found := rp.selfAllocation[deviceID]; found && prevAllocated {
					allocationMap = rp.selfAllocation
					break
				}
			}
		}
	}
	for _, deviceID := range deviceList {
		if _, found := rp.devices[deviceID]; found {
			if prevAllocated, found := allocationMap[deviceID]; (!found && allocated) || (found && (allocated != prevAllocated)) { //nolint:lll
				changed = true
				allocationMap[deviceID] = allocated
			}
		}
	}
	rp.mu.Unlock()
	return changed
}

func (rp *ResourcePoolImpl) GetSelfAllocation() map[string]bool {
	copied := make(map[string]bool)
	rp.mu.Lock()
	for dev, allocated := range rp.selfAllocation {
		copied[dev] = allocated
	}
	rp.mu.Unlock()
	return copied
}

func (rp *ResourcePoolImpl) IsTopologyAware() bool {
	return rp.isTopologyAware
}
