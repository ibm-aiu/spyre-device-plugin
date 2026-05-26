/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice_test

import (
	"time"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("SpyreNodeStates Condition", func() {
	Context("SpyreNodeStateHealthCondition", func() {
		DescribeTable("UpdateCondition", func(hasDevice bool, unhealthyDevices []spyrev1alpha1.UnhealthyDevice,
			previousCondition, expectedCondition metav1.Condition, expectedChanged bool) {
			previousCondition.LastTransitionTime = metav1.NewTime(time.Now().Add(-time.Minute))
			healthCondition := NewSpyreNodeStateHealthCondition(hasDevice, unhealthyDevices)
			changed, newCondition := healthCondition.UpdateCondition(previousCondition.Status, previousCondition.Message, previousCondition.LastTransitionTime)
			Expect(changed).To(Equal(expectedChanged))
			Expect(newCondition.Type).To(Equal(ConditionTypeDeviceHealthy))
			Expect(newCondition.Status).To(Equal(expectedCondition.Status))
			Expect(newCondition.Reason).To(Equal(expectedCondition.Reason))
			Expect(newCondition.Message).To(Equal(expectedCondition.Message))
			if expectedChanged {
				Expect(newCondition.LastTransitionTime.Time.After(
					previousCondition.LastTransitionTime.Time,
				)).To(BeTrue())
			} else {
				Expect(newCondition.LastTransitionTime).To(BeEquivalentTo(previousCondition.LastTransitionTime))
			}
		},
			Entry("unknown to healthy",
				true, []spyrev1alpha1.UnhealthyDevice{},
				metav1.Condition{Status: metav1.ConditionUnknown, Message: MessageNoDetectedDevice},
				metav1.Condition{Status: metav1.ConditionTrue, Reason: ReasonAllDevicesHealthy, Message: MessageAllDeviceHealthy},
				true,
			),
			Entry("healthy to unknown",
				false, []spyrev1alpha1.UnhealthyDevice{},
				metav1.Condition{Status: metav1.ConditionTrue, Message: MessageAllDeviceHealthy},
				metav1.Condition{Status: metav1.ConditionUnknown, Reason: ReasonNoDetectedDevice, Message: MessageNoDetectedDevice},
				true,
			),
			Entry("unhealthy to unknown",
				false, []spyrev1alpha1.UnhealthyDevice{},
				metav1.Condition{Status: metav1.ConditionFalse, Message: func() string {
					return MessageSomeDeviceUnhealthy(1)
				}()},
				metav1.Condition{Status: metav1.ConditionUnknown, Reason: ReasonNoDetectedDevice, Message: MessageNoDetectedDevice},
				true,
			),
			Entry("no change to healthy",
				true, []spyrev1alpha1.UnhealthyDevice{},
				metav1.Condition{Status: metav1.ConditionTrue, Message: MessageAllDeviceHealthy},
				metav1.Condition{Status: metav1.ConditionTrue, Reason: ReasonAllDevicesHealthy, Message: MessageAllDeviceHealthy},
				false,
			),
			Entry("healthy to unhealthy",
				true, []spyrev1alpha1.UnhealthyDevice{{ID: "01", State: pb.DEVICE_STATE_REMOVED.String()}},
				metav1.Condition{Status: metav1.ConditionTrue, Message: MessageAllDeviceHealthy},
				metav1.Condition{Status: metav1.ConditionFalse, Reason: ReasonSomeDeviceUnhealthy, Message: func() string {
					return MessageSomeDeviceUnhealthy(1)
				}()},
				true,
			),
			Entry("unhealthy to healthy",
				true, []spyrev1alpha1.UnhealthyDevice{},
				metav1.Condition{Status: metav1.ConditionFalse, Message: func() string {
					return MessageSomeDeviceUnhealthy(1)
				}()},
				metav1.Condition{Status: metav1.ConditionTrue, Reason: ReasonAllDevicesHealthy, Message: MessageAllDeviceHealthy},
				true,
			),
			Entry("unequal unhealthy",
				true, []spyrev1alpha1.UnhealthyDevice{{ID: "01", State: pb.DEVICE_STATE_REMOVED.String()}},
				metav1.Condition{Status: metav1.ConditionFalse, Message: func() string {
					return MessageSomeDeviceUnhealthy(2)
				}()},
				metav1.Condition{Status: metav1.ConditionFalse, Reason: ReasonSomeDeviceUnhealthy, Message: func() string {
					return MessageSomeDeviceUnhealthy(1)
				}()},
				true,
			),
			Entry("equal unhealthy",
				true, []spyrev1alpha1.UnhealthyDevice{{ID: "01", State: pb.DEVICE_STATE_REMOVED.String()}},
				metav1.Condition{Status: metav1.ConditionFalse, Message: func() string {
					return MessageSomeDeviceUnhealthy(1)
				}()},
				metav1.Condition{Status: metav1.ConditionFalse, Reason: ReasonSomeDeviceUnhealthy, Message: func() string {
					return MessageSomeDeviceUnhealthy(1)
				}()},
				false,
			),
		)
	})

})
