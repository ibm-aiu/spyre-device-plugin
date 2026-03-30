/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/health"
)

var _ = Describe("PCIMonitor", func() {

	It("should correctly compare device maps", func() {
		map1 := map[string]any{
			"0001:00:00.0": struct{}{},
			"0002:00:00.0": struct{}{},
		}
		map2 := map[string]any{
			"0001:00:00.0": struct{}{},
			"0002:00:00.0": struct{}{},
		}
		Expect(MapsEqual(map1, map2)).To(BeTrue())
		map3 := map[string]any{
			"0001:00:00.0": struct{}{},
			"0003:00:00.0": struct{}{},
		}
		Expect(MapsEqual(map1, map3)).To(BeFalse())
		emptyMap := map[string]any{}
		Expect(MapsEqual(nil, nil)).To(BeTrue())
		Expect(MapsEqual(map1, nil)).To(BeFalse())
		Expect(MapsEqual(nil, map1)).To(BeFalse())
		Expect(MapsEqual(map1, emptyMap)).To(BeFalse())
		Expect(MapsEqual(emptyMap, map3)).To(BeFalse())
		Expect(MapsEqual(emptyMap, emptyMap)).To(BeTrue())
	})

})
