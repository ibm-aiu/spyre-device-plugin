/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice

import (
	"fmt"

	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition Types
const (
	ConditionTypeDeviceHealthy = "DeviceHealthy"
)

// Condition Reasons
const (
	ReasonAllDevicesHealthy   = "AllDevicesHealthy"
	ReasonSomeDeviceUnhealthy = "SomeDeviceUnhealthy"
	ReasonNoDetectedDevice    = "NoDetectedDevice"

	MessageAllDeviceHealthy = "All devices are healthy."
	MessageNoDetectedDevice = "No device detected."
)

func MessageSomeDeviceUnhealthy(unhealthyDeviceCount int) string {
	return fmt.Sprintf("%d devices are unhealthy.", unhealthyDeviceCount)
}

type SpyreNodeStateHealthCondition struct {
	hasDevice        bool
	unhealthyDevices []spyrev1alpha1.UnhealthyDevice
}

func NewSpyreNodeStateHealthCondition(hasDevice bool, unhealthyDevices []spyrev1alpha1.UnhealthyDevice) *SpyreNodeStateHealthCondition {
	return &SpyreNodeStateHealthCondition{
		unhealthyDevices: unhealthyDevices,
		hasDevice:        hasDevice,
	}
}

// UpdateCondition gets a new condition and
// return whether there is a change to the input condition, and the new condition.
func (c *SpyreNodeStateHealthCondition) UpdateCondition(status metav1.ConditionStatus,
	message string, lastTransitionTime metav1.Time) (bool, metav1.Condition) {
	newCondition := c.getCondition()
	// no change
	if status == newCondition.Status && message == newCondition.Message {
		newCondition.LastTransitionTime = lastTransitionTime
		return false, newCondition
	}
	newCondition.LastTransitionTime = metav1.Now()
	return true, newCondition
}

func (c *SpyreNodeStateHealthCondition) FirstCondition() metav1.Condition {
	newCondition := c.getCondition()
	newCondition.LastTransitionTime = metav1.Now()
	return newCondition
}

// getCondition initialize a new condition based on unhealthy list and whether there is any device detected.
func (c *SpyreNodeStateHealthCondition) getCondition() metav1.Condition {
	noUnhealthyDevice := len(c.unhealthyDevices) == 0
	newCondition := metav1.Condition{
		Type: ConditionTypeDeviceHealthy,
	}
	switch {
	case noUnhealthyDevice && c.hasDevice:
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = ReasonAllDevicesHealthy
		newCondition.Message = MessageAllDeviceHealthy
	case noUnhealthyDevice && !c.hasDevice:
		newCondition.Status = metav1.ConditionUnknown
		newCondition.Reason = ReasonNoDetectedDevice
		newCondition.Message = MessageNoDetectedDevice
	case !noUnhealthyDevice:
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = ReasonSomeDeviceUnhealthy
		newCondition.Message = MessageSomeDeviceUnhealthy(len(c.unhealthyDevices))
	}
	return newCondition
}
