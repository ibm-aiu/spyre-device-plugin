/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice

import (
	"context"

	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// AllocateFromDeviceMap exports allocateFromDeviceMap for testing
func AllocateFromDeviceMap(availableDeviceIDs []string, nDev int32, deviceMap map[string]*pluginapi.Device) []string {
	return allocateFromDeviceMap(availableDeviceIDs, nDev, deviceMap)
}

// AllocateReservedDevices exports allocateReservedDevices for testing
func AllocateReservedDevices(ctx context.Context, spyreClient *spyreclient.SpyreClient, resourceName string,
	availableDeviceIDs []string, nDev int32) ([]string, error) {
	return allocateReservedDevices(ctx, spyreClient, resourceName, availableDeviceIDs, nDev)
}

// Made with Bob
