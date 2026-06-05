/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	SpyreMetricBaseFolderName = "spyre-metrics"
)

var (
	defaultMetricsHostPath   = filepath.Join(devicePluginPath, SpyreMetricBaseFolderName)
	MetricsHostPathKey       = "SPYRE_METRIC_HOSTPATH"
	MetricsOutputPathKey     = "SPYRE_METRIC_FILEPATH"
	defaultMetricsOutputPath = "/tmp/spyre-metrics"
)

var metricsHostPath string
var metricsContainerPath string

// InitMetricsMountPath initializes path host/container path for spyre metrics
func InitMetricsMountPath() error {
	metricsHostPath = GetMetricsHostPath()
	metricsContainerPath = GetMetricsContainerPath()
	return utils.CreateFolderIfNotExists(metricsHostPath)
}

func GetMetricsHostPath() string {
	return utils.GetEnvOrDefault(MetricsHostPathKey, defaultMetricsHostPath)
}

func GetMetricsContainerPath() string {
	return utils.GetEnvOrDefault(MetricsOutputPathKey, defaultMetricsOutputPath)
}

func getMetricsMount(configHostMntPath string) (*pluginapi.Mount, error) {
	outputPath, err := utils.CreateNewMetricsFolder(metricsHostPath, configHostMntPath)
	if err != nil {
		return nil, err
	}
	return &pluginapi.Mount{
		ContainerPath: metricsContainerPath,
		HostPath:      outputPath,
		ReadOnly:      false,
	}, nil
}

func IsMetricsMnt(containerMntPath, hostMntPath string) bool {
	targetMetricsPath := GetMetricsContainerPath()
	return containerMntPath == targetMetricsPath && strings.Contains(hostMntPath, SpyreMetricBaseFolderName)
}

func WritePodInfo(mntHostPaths []string, pod corev1.Pod) error {
	for _, hostMntPath := range mntHostPaths {
		if strings.Contains(hostMntPath, SpyreConfigBaseFolderName) {
			if err := writeInfoFiles(hostMntPath, pod); err != nil {
				return fmt.Errorf("error writing pod info to %s: %v", hostMntPath, err)
			}
			return nil
		}
	}
	return nil
}
