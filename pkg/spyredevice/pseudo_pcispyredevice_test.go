/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice

import (
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var _ = Describe("PseudoPciDevice", func() {
	It("can instantiate PseudoPciDevice object", func() {
		d := PseudoPciDevice{ProductID: "06a7"}
		Expect(d.GetDeviceCode()).Should(Equal("06a7"))
		d = PseudoPciDevice{ProductID: "06a8"}
		Expect(d.GetDeviceCode()).Should(Equal("06a8"))
	})
	It("can instantiate PseudoPciDevice object with specified PCI address", func() {
		d := PseudoPciDevice{PciAddress: "abc", ProductID: "06a7"}
		Expect(d.GetDeviceCode()).Should(Equal("06a7"))
		Expect(d.GetPciAddr()).Should(Equal("abc"))

	})
	It("can instantiate PseudoPciDevice object with isolated VF with specified PCI address", func() {
		d := PseudoPciDevice{PciAddress: "0001:00:00.0", ProductID: "06a8", pfAddr: ""}
		Expect(d.GetPciAddr()).Should(Equal("0001:00:00.0"))
		Expect(d.GetDeviceCode()).Should(Equal("06a8"))
		Expect(d.IsIsolatedVF()).Should(BeTrue())

		// Test regular VF (should not be isolated)
		d2 := PseudoPciDevice{PciAddress: "0000:1a:00.1", ProductID: "06a8", pfAddr: "0000:1a:00.0"}
		Expect(d2.IsIsolatedVF()).Should(BeFalse())

		// Test another isolated VF address
		d3 := PseudoPciDevice{PciAddress: "0002:00:00.0", ProductID: "06a8", pfAddr: ""}
		Expect(d3.IsIsolatedVF()).Should(BeTrue())
	})
	It("can set Health of PseudoPciDevice object", func() {
		d := NewPseudoPciDevice(GeneratePseudoDevice("0002:00:00.0", resources.PfProductId))
		Expect(d.GetHealth()).To(Equal(pluginapi.Healthy))
		d.SetHealth(pluginapi.Unhealthy)
		Expect(d.GetHealth()).To(Equal(pluginapi.Unhealthy))
	})
})
