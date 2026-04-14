/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	SpyreConfigBaseFolderName = "spyre-config"
)

var (
	devicePluginPath = "/usr/local/etc/device-plugins"
	defaultHostPath  = filepath.Join(devicePluginPath, SpyreConfigBaseFolderName)

	HostPathKey           = "CONFIG_HOSTPATH"
	OutputPathKey         = "CONFIG_FILEPATH"
	ConfigFileNameKey     = "CONFIG_FILENAME"
	defaultOutputPath     = "/etc/aiu"
	defaultConfigFileName = "senlib_config.json"
)

var senlibConfigGenerator SenlibConfigGenerator
var configHostPath string
var configContainerPath string

type ConfigHandler struct {
	// uuidMap maps from deviceIDs to generated uuid
	uuidMap map[string]string
}

func uniqueStringFromDeviceIDs(deviceIDs []string) string {
	return strings.Join(deviceIDs, "-")
}

func GetConfigContainerPath() string {
	return utils.GetEnvOrDefault(OutputPathKey, defaultOutputPath)
}

func GetConfigHostPath() string {
	return utils.GetEnvOrDefault(HostPathKey, defaultHostPath)
}

func GetConfigFileName() string {
	return utils.GetEnvOrDefault(ConfigFileNameKey, defaultConfigFileName)
}

// InitConfigMount prepares config folder and return handler
func InitConfigMount() (*ConfigHandler, error) {
	cfgHandler := &ConfigHandler{make(map[string]string)}
	configHostPath = GetConfigHostPath()
	configContainerPath = GetConfigContainerPath()
	if err := InitMetricsMountPath(); err != nil {
		return cfgHandler, err
	}
	var err error
	if senlibConfigGenerator, err = NewSenlibConfigGenerator(); err != nil {
		return cfgHandler, err
	}
	err = utils.CreateFolderIfNotExists(configHostPath)
	return cfgHandler, err
}

// GetConfigMetricsMount returns config and metrics mounts
func (h *ConfigHandler) GetConfigMetricsMount(resourcePool string,
	deviceIDs []string) (mnts []*pluginapi.Mount, err error) {
	// create new folder on each container requests
	outputPath, err := utils.CreateNewConfigFolder(configHostPath)
	if err != nil {
		return mnts, err
	}
	if utils.IsTopologyAwareResource(resourcePool) {
		if err := CopyTopologyFile(outputPath); err != nil {
			glog.Warningf("cannot copy topology file to %s: %v, gracefully skip", outputPath, err)
		}
	}
	configMnt, err := getSenlibConfigMount(resourcePool, deviceIDs, outputPath)
	if err != nil {
		return mnts, err
	}
	mnts = append(mnts, configMnt)
	if !IsMetricsEnabled() {
		return mnts, nil
	}
	metricsMnt, err := getMetricsMount(configMnt.HostPath)
	if err == nil {
		mnts = append(mnts, metricsMnt)
		key := uniqueStringFromDeviceIDs(deviceIDs)
		h.uuidMap[key] = outputPath
	}
	return mnts, err
}

func IsMetricsEnabled() bool {
	return senlibConfigGenerator.metricEnabled
}

// Still outstanding: clean up folder after permanently delete device plugin pod. Might be called
// via some API from controller when cluster policy is deleted.
func getSenlibConfigMount(resourcePool string, deviceIDs []string, outputPath string) (*pluginapi.Mount, error) {
	err := senlibConfigGenerator.GenerateConfigFile(resourcePool, deviceIDs, outputPath)
	if err != nil {
		return nil, err
	}
	return &pluginapi.Mount{
		ContainerPath:        configContainerPath,
		HostPath:             outputPath,
		ReadOnly:             true,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}, nil
}

func CopyTopologyFile(outputPath string) error {
	dst := fmt.Sprintf("%s/topo.json", outputPath)
	// Use current topology file which prefers dynamic over original
	topologyFilepath := spyretopo.GetCurrentTopologyFile()
	data, err := os.ReadFile(topologyFilepath)
	if err != nil {
		return fmt.Errorf("failed to read topology file %s: %w", topologyFilepath, err)
	}
	var topoData map[string]interface{}
	if err := json.Unmarshal(data, &topoData); err != nil {
		glog.Warningf("Failed to parse topology file as JSON, falling back to copy: %v", err)
		return utils.CopyFile(topologyFilepath, dst)
	}
	numDevicesVal, ok := topoData["num_devices"].(float64)
	if !ok {
		glog.Warning("num_devices missing or not numeric, copying original file")
		return utils.CopyFile(topologyFilepath, dst)
	}
	// isolated VF case
	if numDevicesVal == 0 {
		if spyreVfNumDevicesVal, ok := topoData["spyre_vf_num_devices"].(float64); ok && spyreVfNumDevicesVal > 0 {
			topoData["num_devices"] = spyreVfNumDevicesVal
			updatedData, err := json.MarshalIndent(topoData, "", "    ")
			if err != nil {
				return fmt.Errorf("failed to marshal updated topology: %w", err)
			}
			if err := os.WriteFile(dst, updatedData, 0644); err != nil {
				return fmt.Errorf("failed to write updated topology file %s: %w", dst, err)
			}
			return nil
		}
	}
	// For all other cases, just copy the original file
	return utils.CopyFile(topologyFilepath, dst)
}

func ListAllMounts(hostPath string) ([]string, error) {
	files, err := os.ReadDir(hostPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", hostPath, err)
	}

	mnts := []string{}
	for _, file := range files {
		if file.IsDir() {
			mnt := filepath.Join(hostPath, file.Name())
			mnts = append(mnts, mnt)
		}
	}
	return mnts, nil
}

func IsConfigHostPathExist() bool {
	hostpath := GetConfigHostPath()
	_, err := os.Stat(hostpath)
	return err == nil
}

func IsSomeContainerMounted() bool {
	hostpath := GetConfigHostPath()
	files, err := os.ReadDir(hostpath)
	if err == nil {
		return len(files) > 0
	}
	return false
}

func IsConfigMnt(containerMntPath, hostMntPath string) bool {
	targetConfigPath := GetConfigContainerPath()
	return containerMntPath == targetConfigPath && strings.Contains(hostMntPath, SpyreConfigBaseFolderName)
}
