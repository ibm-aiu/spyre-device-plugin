/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package manager

import (
	"context"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"k8s.io/client-go/rest"
)

func (rm *ResourceManager) ExportResourceServers() []types.ResourceServer {
	return rm.resourceServers
}

func (rm *ResourceManager) ExportFilteredDevices() []types.PciDevice {
	return rm.filteredDevices
}

func (rm *ResourceManager) ExportDeviceProviders() map[types.DeviceType]types.DeviceProvider {
	return rm.deviceProviders
}

func (rm *ResourceManager) ExportConfigList() []*types.ResourceConfig {
	return rm.configList
}

func (rm *ResourceManager) TestHandleHotpluggedDevices(
	ctx context.Context, cfg *rest.Config, spyreClient *spyreclient.SpyreClient, newDevices []types.PciDevice) {
	rm.HandleHotpluggedDevices(ctx, cfg, spyreClient, newDevices)
}

func (rm *ResourceManager) TestAddNewDevicesToProvider(newDevices []types.PciDevice) error {
	return rm.addNewDevicesToProvider(newDevices)
}

func (rm *ResourceManager) TestAddPerDeviceServersForNewDevices(newDevices []types.PciDevice) {
	rm.addPerDeviceServersForNewDevices(newDevices)
}

func (rm *ResourceManager) TestUpdateAggregatedResourcePools(newDevices []types.PciDevice) {
	rm.updateAggregatedResourcePools(newDevices)
}

func ExportContainsPfOrVfDevices(newDevices []types.PciDevice) (bool, bool) {
	return containsPfOrVfDevices(newDevices)
}

func ExportExtractPCIAddr(resourceName string) string {
	return extractPCIAddr(resourceName)
}

func ExportFilterDevicesByPCIAddr(devices []types.PciDevice, addr string) []types.PciDevice {
	return filterDevicesByPCIAddr(devices, addr)
}

// ExportNewResourceManagerWithConfig exposes NewResourceManagerWithConfig for testing
func ExportNewResourceManagerWithConfig(
	cp *CliParams, spyreClient *spyreclient.SpyreClient, cfg *rest.Config) *ResourceManager {
	return NewResourceManagerWithConfig(cp, spyreClient, cfg)
}
