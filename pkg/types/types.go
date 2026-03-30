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

package types

import (
	"encoding/json"

	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	"github.com/jaypipes/ghw"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var (
	// SockDir is the default Kubelet device plugin socket directory
	SockDir = "/var/lib/kubelet/plugins_registry"
	// DeprecatedSockDir is the deprecated Kubelet device plugin socket directory
	DeprecatedSockDir = "/var/lib/kubelet/device-plugins"
)

const (
	// KubeEndPoint is kubelet socket name
	KubeEndPoint = "kubelet.sock"
)

// DeviceType is custom type to define supported device types
type DeviceType string

const (
	// SpyreDeviceType is DeviceType for class devices
	SpyreDeviceType DeviceType = "spyreDevice"
)

// SupportedDevices is map of 'device identifier as string' to 'device class hexcode as int'
/*
Supported PCI Device Classes. ref: https://pci-ids.ucw.cz/read/PD
12	Processing accelerators

Processing accelerators subclasses. ref: https://pci-ids.ucw.cz/read/PD/12
00	Processing accelerators
01	AI Inference Accelerator
*/
var SupportedDevices = map[DeviceType][]int64{
	SpyreDeviceType: {0x00, 0x12},
}

// ResourceConfig contains configuration parameters for a resource pool
type ResourceConfig struct {
	// optional resource prefix that will overwrite	global prefix specified in cli params
	ResourcePrefix string `json:"resourcePrefix,omitempty"`
	// the resource name will be added with resource prefix in K8s api
	ResourceName string           `json:"resourceName"`
	DeviceType   DeviceType       `json:"deviceType,omitempty"`
	Selectors    *json.RawMessage `json:"selectors,omitempty"`
	SelectorObj  interface{}
}

// DeviceSelectors contains common device selectors fields
type DeviceSelectors struct {
	Vendors      []string `json:"vendors,omitempty"`
	Devices      []string `json:"devices,omitempty"`
	Drivers      []string `json:"drivers,omitempty"`
	PciAddresses []string `json:"pciAddresses,omitempty"`
}

// ResourceConfList is list of ResourceConfig
type ResourceConfList struct {
	// config file: "resourceList" :[{<ResourceConfig configs>},{},{},...]
	ResourceList []ResourceConfig `json:"resourceList"`
}

// ResourceServer is gRPC server implements K8s device plugin api
type ResourceServer interface {
	// Device manager API
	pluginapi.DevicePluginServer
	// grpc server related
	Start() error
	Stop() error
	// Init initializes resourcePool
	Init() error
	// Watch watches for socket file deletion and restart server if needed
	Watch()
	GetResourcePool() ResourcePool
	InformedBySharedInfo(deviceList []string, allocated bool, self bool)
	TriggerUpdate()
	GetPciTopology() *pcitopov2.Pcitopo
	WaitForNoAllocationInProcess()
}

type AllocationInfo struct {
	DeviceIDs    []string
	MountPoints  []string
	ResourceName string
}

type DeallocationInfo struct {
	DeviceIDs    []string
	ResourceName string
}

// ResourceFactory is an interface to get instances of ResourcePool and ResourceServer
type ResourceFactory interface {
	GetResourceServer(ResourcePool) (ResourceServer, error)
	GetDefaultInfoProvider(string, string) DeviceInfoProvider
	GetSelector(string, []string) (DeviceSelector, error)
	GetResourcePool(rc *ResourceConfig, deviceList []PciDevice) (ResourcePool, error)
	GetDeviceProvider(DeviceType) DeviceProvider
	GetDeviceFilter(*ResourceConfig) (interface{}, error)
	StopSharedInfo()
	GetAllocateCh() chan AllocationInfo
	GetMountedCh() chan []string
	GetDeallocateCh() chan DeallocationInfo
}

// ResourcePool represents a generic resource entity
type ResourcePool interface {
	// extended API for internal use
	GetResourceName() string
	GetResourcePrefix() string
	GetDevices() map[string]*pluginapi.Device // for ListAndWatch
	Probe() bool
	GetDeviceSpecs(deviceIDs []string) []*pluginapi.DeviceSpec
	GetEnvs(deviceIDs []string) []string
	GetMounts(deviceIDs []string) []*pluginapi.Mount
	InformedBySharedInfo(deviceList []string, allocated bool, self bool) bool
	// IsTopologyAware returns true if contains `tier` in the resource name
	IsTopologyAware() bool
	GetSelfAllocation() map[string]bool
}

// DeviceProvider provides interface for device discovery
type DeviceProvider interface {
	// AddTargetDevices adds a list of devices in a DeviceProvider that matches the
	// 'device class hexcode as int'
	AddTargetDevices([]*ghw.PCIDevice, []int64) error
	GetDiscoveredDevices() []*ghw.PCIDevice

	// GetDevices runs through the Discovered Devices and returns a list of fully populated
	// PciDevices according to the given ResourceConfig
	GetDevices(*ResourceConfig) []PciDevice

	GetFilteredDevices([]PciDevice, *ResourceConfig) ([]PciDevice, error)

	// ValidConfig performs validation of DeviceType-specific configuration
	ValidConfig(*ResourceConfig) bool
}

// PciDevice provides an interface to get generic device specific information
type PciDevice interface {
	GetVendor() string
	GetDriver() string
	GetDeviceCode() string
	GetPciAddr() string
	GetPfPciAddr() string
	IsSriovPF() bool
	GetDeviceSpecs() []*pluginapi.DeviceSpec
	GetEnvVal() string
	GetMounts() []*pluginapi.Mount
	GetAPIDevice() *pluginapi.Device
	SetHealth(string)
	GetHealth() string
	GetNumaInfo() string
	IsIsolatedVF() bool
}

// DeviceInfoProvider is an interface to get Device Plugin API specific device information
type DeviceInfoProvider interface {
	GetDeviceSpecs() []*pluginapi.DeviceSpec
	GetEnvVal() string
	GetMounts() []*pluginapi.Mount
}

// DeviceSelector provides an interface for filtering a list of devices
type DeviceSelector interface {
	Filter([]PciDevice) []PciDevice
}
