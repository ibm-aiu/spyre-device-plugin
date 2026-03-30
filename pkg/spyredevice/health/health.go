/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
)

type DeviceHealthState struct {
	state      pb.DEVICE_STATE
	deviceType pb.DEVICE_TYPE
}

// NewDeviceHealthState returns a new DeviceHealthState with online (healthy) state
func NewDeviceHealthState(deviceType pb.DEVICE_TYPE) *DeviceHealthState {
	return &DeviceHealthState{
		state:      pb.DEVICE_STATE_ONLINE,
		deviceType: deviceType,
	}
}

func (d *DeviceHealthState) SetHealthState(state pb.DEVICE_STATE) {
	d.state = state
}

func (d *DeviceHealthState) Healthy() bool {
	return d.state == pb.DEVICE_STATE_ONLINE
}

func (d *DeviceHealthState) GetHealthState() pb.DEVICE_STATE {
	return d.state
}

func (d *DeviceHealthState) IsSriovPF() bool {
	return d.deviceType == pb.DEVICE_TYPE_PF
}
