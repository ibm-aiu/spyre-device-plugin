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

package factory

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources/server"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
)

type resourceFactory struct {
	endPointPrefix   string
	endPointSuffix   string
	pluginWatch      bool
	sharedInfo       *SpyreDeviceSharedInfo
	topologyFilepath string
	spyreClient      *spyreclient.SpyreClient
}

var instance *resourceFactory

// NewResourceFactory returns an instance of Resource Server factory
func NewResourceFactory(prefix, suffix string, pluginWatch bool, topologyFilepath string, spyreClient *spyreclient.SpyreClient) types.ResourceFactory { //nolint:lll
	if instance == nil {
		rf := &resourceFactory{
			endPointPrefix:   prefix,
			endPointSuffix:   suffix,
			pluginWatch:      pluginWatch,
			sharedInfo:       NewSpyreDeviceSharedInfo(),
			topologyFilepath: topologyFilepath,
			spyreClient:      spyreClient,
		}
		go rf.sharedInfo.Run()
		return rf
	}
	return instance
}

// GetResourceServer returns an instance of ResourceServer for a ResourcePool
func (rf *resourceFactory) GetResourceServer(rp types.ResourcePool) (types.ResourceServer, error) {
	if rp != nil {
		prefix := rf.endPointPrefix
		if prefixOverride := rp.GetResourcePrefix(); prefixOverride != "" {
			prefix = prefixOverride
		}
		server := server.NewResourceServer(prefix, rf.endPointSuffix, rf.pluginWatch, rp, rf.spyreClient,
			rf.sharedInfo.allocatedCh, rf.topologyFilepath)
		rf.sharedInfo.register(server)
		return server, nil
	}
	return nil, fmt.Errorf("factory: unable to get resource pool object")
}

// GetDefaultInfoProvider returns an instance of DeviceInfoProvider using name as string
func (rf *resourceFactory) GetDefaultInfoProvider(pciAddr, name string) types.DeviceInfoProvider {
	switch name {
	case "vfio-pci":
		return resources.NewVfioInfoProvider(pciAddr)
	default:
		return resources.NewGenericInfoProvider(pciAddr)
	}
}

// GetSelector returns an instance of DeviceSelector using selector attribute string and its associated values
func (rf *resourceFactory) GetSelector(attr string, values []string) (types.DeviceSelector, error) {
	switch attr {
	case "vendors":
		return resources.NewVendorSelector(values), nil
	case "devices":
		return resources.NewDeviceSelector(values), nil
	case "drivers":
		return resources.NewDriverSelector(values), nil
	case "pciAddresses":
		return resources.NewPciAddressSelector(values), nil
	default:
		return nil, fmt.Errorf("GetSelector(): invalid attribute %s", attr)
	}
}

// GetResourcePool returns an instance of resourcePool
func (rf *resourceFactory) GetResourcePool(rc *types.ResourceConfig,
	filteredDevice []types.PciDevice) (types.ResourcePool, error) {

	devicePool := make(map[string]types.PciDevice)
	apiDevices := make(map[string]*pluginapi.Device)
	for _, dev := range filteredDevice {
		pciAddr := dev.GetPciAddr()
		devicePool[pciAddr] = dev
		apiDevices[pciAddr] = dev.GetAPIDevice()
		glog.Infof("device added: [pciAddr: %s, vendor: %s, device: %s, driver: %s]",
			dev.GetPciAddr(),
			dev.GetVendor(),
			dev.GetDeviceCode(),
			dev.GetDriver())
	}

	var rPool types.ResourcePool
	var err error
	switch rc.DeviceType {
	case types.SpyreDeviceType:
		if len(filteredDevice) > 0 {
			rPool = spyredevice.NewSpyreResourcePool(rc, apiDevices, devicePool)
		}
	default:
		err = fmt.Errorf("cannot create resourcePool: invalid device type %s", rc.DeviceType)
	}
	return rPool, err
}

// GetDeviceProvider returns an instance of DeviceProvider based on DeviceType
func (rf *resourceFactory) GetDeviceProvider(dt types.DeviceType) types.DeviceProvider {
	switch dt {
	case types.SpyreDeviceType:
		return spyredevice.NewSpyreDeviceProvider(rf)
	default:
		return nil
	}
}

// GetDeviceFilter unmarshal the "selector" values from ResourceConfig and returns an instance of DeviceSelector based
// on DeviceType in the ResourceConfig
func (rf *resourceFactory) GetDeviceFilter(rc *types.ResourceConfig) (interface{}, error) {
	switch rc.DeviceType {
	case types.SpyreDeviceType:
		deviceSelector := &types.DeviceSelectors{}

		if err := json.Unmarshal(*rc.Selectors, deviceSelector); err != nil {
			return nil, fmt.Errorf("error unmarshalling SpyreDevice selector bytes %v", err)
		}

		glog.V(1).Infof("device selector for resource %s is %+v", rc.ResourceName, deviceSelector)
		return deviceSelector, nil
	default:
		return nil, fmt.Errorf("unable to get deviceFilter, invalid deviceType %s", rc.DeviceType)
	}
}

func (rf *resourceFactory) StopSharedInfo() {
	rf.sharedInfo.stop <- true
}

func (rf *resourceFactory) GetAllocateCh() chan types.AllocationInfo {
	return rf.sharedInfo.allocatedCh
}

func (rf *resourceFactory) GetDeallocateCh() chan types.DeallocationInfo {
	return rf.sharedInfo.deallocatedCh
}

func (rf *resourceFactory) GetMountedCh() chan []string {
	return rf.sharedInfo.mountedCh
}
