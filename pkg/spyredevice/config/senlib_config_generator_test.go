/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package config_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	pfResourcePool = "spyre_pf"
	vfResourcePool = "spyre_vf"

	expectedRisvDisabledContent = `"SNT_MCI":{"DCR":{"MCI_CTRL":{"ENABLE_RISCV":"0x0"}}}`
)

var newGeneratorErrs = []string{"error opening", "unmarshal"}

var _ = Describe("SenlibConfigGenerator", func() {
	DescribeTable("generator test", func(tcSubFolder, resourcePool string, busIds []string, metricEnabled bool, expectedMetricPath string, expectedRisvDisabled bool, expectedError error) {
		os.Setenv(TemplatePathKey, fmt.Sprintf("../../../test/data/senlib_config/%s", tcSubFolder))
		defer os.Unsetenv(TemplatePathKey)
		generator, err := NewSenlibConfigGenerator()
		var isNewGeneratorErr, isFormatErr bool
		if expectedError != nil {
			if slices.Contains(newGeneratorErrs, expectedError.Error()) {
				isNewGeneratorErr = true
			} else {
				isFormatErr = true
			}
		}
		if isNewGeneratorErr {
			Expect(err).NotTo(BeNil())
			Expect(strings.Contains(err.Error(), expectedError.Error())).To(BeTrue())
			return
		}
		content, err := generator.GenerateConfigContent(resourcePool, busIds, "/tmp/spyre-metrics")
		if isFormatErr {
			Expect(err).NotTo(BeNil())
			Expect(strings.Contains(err.Error(), expectedError.Error())).To(BeTrue())
			return
		}
		Expect(err).To(BeNil())
		var senlibConfig SenlibConfig
		if expectedRisvDisabled {
			Expect(string(content)).To(ContainSubstring(expectedRisvDisabledContent))
		} else {
			Expect(string(content)).NotTo(ContainSubstring(expectedRisvDisabledContent))
		}
		err = json.Unmarshal(content, &senlibConfig)
		Expect(err).To(BeNil())
		configuredBusIds := senlibConfig.General.PciAddresses
		Expect(len(configuredBusIds)).To(Equal(len(busIds)))
		for i := range busIds {
			Expect(busIds[i]).To(BeEquivalentTo(configuredBusIds[i]))
		}
		expectedDoom := strings.Contains(resourcePool, "spyre_vf")
		Expect(senlibConfig.General.Doom).To(Equal(expectedDoom))
		Expect(senlibConfig.Metric.General.Enable).To(Equal(metricEnabled))
		Expect(senlibConfig.Metric.General.Path).To(Equal(expectedMetricPath))
	},
		Entry("single Spyre, disable metrics", "disable", vfResourcePool, []string{"01"}, false, WildCardLocalMetricPath, false, nil),
		Entry("multiple Spyres, disable metrics", "disable", vfResourcePool, []string{"01", "02"}, false, WildCardLocalMetricPath, false, nil),
		Entry("single Spyre, enable metrics", "enable", vfResourcePool, []string{"01"}, true, "/tmp/spyre-metrics/metrics.%BUSID", false, nil),
		Entry("multiple Spyre, enable metrics", "enable", vfResourcePool, []string{"01", "02"}, true, "/tmp/spyre-metrics/metrics.%BUSID", false, nil),
		Entry("single Spyre, no METRICS.general defined", "only-metrics", vfResourcePool, []string{"01"}, false, WildCardLocalMetricPath, false, nil),
		Entry("multiple Spyre, no METRICS.general defined", "only-metrics", vfResourcePool, []string{"01", "02"}, false, WildCardLocalMetricPath, false, nil),
		Entry("single Spyre, no METRICS defined", "only-general", vfResourcePool, []string{"01"}, false, WildCardLocalMetricPath, false, nil),
		Entry("multiple Spyre, no METRICS defined", "only-general", vfResourcePool, []string{"01", "02"}, false, WildCardLocalMetricPath, false, nil),
		Entry("empty", "empty", vfResourcePool, []string{"01"}, false, "", false, NoGeneralKeyErr),
		Entry("wrong format", "wrong-format", vfResourcePool, []string{"01"}, false, "", false, errors.New("unmarshal")),
		Entry("wrong format of GENERAL", "wrong-format-general", vfResourcePool, []string{"01"}, false, "", false, errors.New("failed to parse GENERAL:")),
		Entry("wrong format of METRICS", "wrong-format-metrics", vfResourcePool, []string{"01"}, false, "", false, errors.New("failed to parse METRICS:")),
		Entry("wrong format of METRICS.general", "wrong-format-metrics-general", vfResourcePool, []string{"01"}, false, "", false, errors.New("failed to parse METRICS.general:")),
		Entry("wrong template path", "wrong-path", vfResourcePool, []string{"01"}, false, "", false, errors.New("error opening")),
		Entry("unknown Spyre, enable metrics", "enable", vfResourcePool, []string{""}, true, "/tmp/spyre-metrics/metrics.%BUSID", false, nil),
		Entry("PF mode", "disable", pfResourcePool, []string{"01"}, false, WildCardLocalMetricPath, true, nil),
		Entry("PF mode with riscv-enabled template", "riscv-enable", pfResourcePool, []string{"01"}, false, WildCardLocalMetricPath, true, nil),
		Entry("VF mode with riscv-enabled template", "riscv-enable", vfResourcePool, []string{"01"}, false, WildCardLocalMetricPath, false, nil),
	)

	It("set different metrics path", func() {
		os.Setenv(TemplatePathKey, fmt.Sprintf("../../../test/data/senlib_config/%s", "enable"))
		generator, err := NewSenlibConfigGenerator()
		Expect(err).To(BeNil())
		content, err := generator.GenerateConfigContent(pfResourcePool, []string{"01"}, "/data")
		Expect(err).To(BeNil())
		var senlibConfig SenlibConfig
		err = json.Unmarshal(content, &senlibConfig)
		Expect(err).To(BeNil())
		Expect(senlibConfig.Metric.General.Enable).To(BeTrue())
		Expect(senlibConfig.Metric.General.Path).To(Equal("/data/metrics.%BUSID"))
		os.Unsetenv(TemplatePathKey)
	})
})
