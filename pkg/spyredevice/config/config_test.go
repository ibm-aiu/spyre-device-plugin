/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	pfTopology = map[string]interface{}{
		"num_devices":          float64(2),
		"spyre_vf_num_devices": float64(0),
		"devices": map[string]interface{}{
			"0001:00:00.0": map[string]interface{}{
				"device_id": "06a8",
			},
		},
		"server":  "test-server",
		"version": 2.0,
	}
)

var _ = Describe("Config", func() {
	tempDir := ""
	originalMetadataPath := ""

	var _ = BeforeEach(func() {
		_ = os.Setenv(TemplatePathKey, TestTemplatePath)
		_ = os.Setenv(HostPathKey, TestConfigHostPath)
		_ = os.Setenv(MetricsHostPathKey, TestMetricsHostPath)
		var err error
		Handler, err = InitConfigMount()
		Expect(err).To(BeNil())

		By("preparing topology file")
		originalMetadataPath = topology.MetadataTopologyFilepath
		tempDir, err := os.MkdirTemp("", "topology_test")
		Expect(err).To(BeNil())
		createAndSetMetadataTopologyFile(pfTopology, tempDir)
	})

	var _ = AfterEach(func() {
		err := os.RemoveAll(TestConfigHostPath)
		Expect(err).To(BeNil())
		err = os.RemoveAll(TestMetricsHostPath)
		Expect(err).To(BeNil())
		_ = os.RemoveAll(tempDir)
		topology.MetadataTopologyFilepath = originalMetadataPath
	})

	It("get correct config mount with copied topology file when metrics is disabled", func() {
		mnts, err := Handler.GetConfigMetricsMount("spyre_pf_tier0", PseudoBusIds)
		Expect(err).To(BeNil())
		Expect(len(mnts)).To(Equal(1))
		checkParentHostPath(mnts[0].HostPath, TestConfigHostPath)
		Expect(mnts[0].ContainerPath).To(BeEquivalentTo(ConfigContainerPath))
		outputFile := filepath.Join(mnts[0].HostPath, "topo.json")
		_, err = os.Stat(outputFile)
		Expect(err).To(BeNil())
	})

	It("skip copying topology file when requesting spyre_pf", func() {
		mnts, err := Handler.GetConfigMetricsMount("spyre_pf", PseudoBusIds)
		Expect(err).To(BeNil())
		Expect(len(mnts)).To(Equal(1))
		checkParentHostPath(mnts[0].HostPath, TestConfigHostPath)
		outputFile := filepath.Join(mnts[0].HostPath, "topo.json")
		_, err = os.Stat(outputFile)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("get correct config and metrics mounts when metrics is enabled", func() {
		_ = os.Setenv(TemplatePathKey, "../../../test/data/senlib_config/enable")
		var err error
		Handler, err = InitConfigMount()
		Expect(err).To(BeNil())

		mnts, err := Handler.GetConfigMetricsMount("spyre_pf", PseudoBusIds)
		Expect(err).To(BeNil())
		Expect(len(mnts)).To(Equal(2))
		checkParentHostPath(mnts[0].HostPath, TestConfigHostPath)
		Expect(mnts[0].ContainerPath).To(BeEquivalentTo(ConfigContainerPath))
		checkParentHostPath(mnts[1].HostPath, TestMetricsHostPath)
		Expect(mnts[1].ContainerPath).To(BeEquivalentTo(MetricsContainerPath))
	})
})

var _ = Describe("CopyTopologyFile", func() {
	var tempDir string
	var testOutputDir string
	var originalMetadataPath string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "topology_test")
		Expect(err).To(BeNil())
		testOutputDir, err = os.MkdirTemp("", "output_test")
		Expect(err).To(BeNil())
		originalMetadataPath = topology.MetadataTopologyFilepath
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
		_ = os.RemoveAll(testOutputDir)
		topology.MetadataTopologyFilepath = originalMetadataPath
	})

	Context("when topology file has isolated VF devices", func() {
		It("should update num_devices when num_devices is 0 and spyre_vf_num_devices > 0", func() {
			testTopo := map[string]interface{}{
				"num_devices":          float64(0),
				"spyre_vf_num_devices": float64(4),
				"devices":              nil,
				"spyre_vf_devices": map[string]interface{}{
					"0001:00:00.0": map[string]interface{}{
						"device_id": "06a8",
						"vendor_id": "IBM",
					},
				},
				"server":  "test-server",
				"version": 2.0,
			}
			createAndSetMetadataTopologyFile(testTopo, tempDir)
			err := CopyTopologyFile(testOutputDir)
			Expect(err).To(BeNil())
			outputFile := filepath.Join(testOutputDir, "topo.json")
			_, err = os.Stat(outputFile)
			Expect(err).To(BeNil())
			outputData, err := os.ReadFile(outputFile)
			Expect(err).To(BeNil())
			var outputTopo map[string]interface{}
			err = json.Unmarshal(outputData, &outputTopo)
			Expect(err).To(BeNil())
			Expect(outputTopo["num_devices"]).To(Equal(float64(4)))
			Expect(outputTopo["spyre_vf_num_devices"]).To(Equal(float64(4)))
			Expect(outputTopo["server"]).To(Equal("test-server"))
			Expect(outputTopo["version"]).To(Equal(2.0))
		})
	})

	Context("when topology file has PFs", func() {
		It("should copy the file unchanged when num_devices > 0", func() {
			createAndSetMetadataTopologyFile(pfTopology, tempDir)
			err := CopyTopologyFile(testOutputDir)
			Expect(err).To(BeNil())
			outputFile := filepath.Join(testOutputDir, "topo.json")
			outputData, err := os.ReadFile(outputFile)
			Expect(err).To(BeNil())
			var outputTopo map[string]interface{}
			err = json.Unmarshal(outputData, &outputTopo)
			Expect(err).To(BeNil())
			Expect(outputTopo["num_devices"]).To(Equal(float64(2)))
			Expect(outputTopo["spyre_vf_num_devices"]).To(Equal(float64(0)))
		})
	})
})

func createAndSetMetadataTopologyFile(testTopo map[string]interface{}, tempDir string) {
	topoFile := filepath.Join(tempDir, "topo.json")
	topoData, err := json.MarshalIndent(testTopo, "", "    ")
	Expect(err).To(BeNil())
	err = os.WriteFile(topoFile, topoData, 0644)
	Expect(err).To(BeNil())
	topology.MetadataTopologyFilepath = topoFile
	topologyFile := topology.GetTopologyFile()
	Expect(topologyFile).To(BeEquivalentTo(topoFile))
}

func checkParentHostPath(path string, expectedParent string) {
	hostPathSplit := strings.Split(path, "/")
	Expect(len(hostPathSplit)).To(BeNumerically(">", 1))
	parentHostPath := filepath.Join(hostPathSplit[0 : len(hostPathSplit)-1]...)
	Expect(parentHostPath).Should(BeEquivalentTo(expectedParent))
}
