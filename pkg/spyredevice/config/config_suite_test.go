/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package config

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	TestConfigHostPath  = "config-host-path"
	TestMetricsHostPath = "metrics-host-path"
	TestTemplatePath    = "../../../test/data/senlib_config/disable"
	PseudoBusIds        = []string{
		"0000_00_0a.0", "0000_00_09.0",
	}
	ConfigContainerPath  = defaultOutputPath
	MetricsContainerPath = defaultMetricsOutputPath
	WriteInfoFiles       = writeInfoFiles
	Handler              *ConfigHandler
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}
