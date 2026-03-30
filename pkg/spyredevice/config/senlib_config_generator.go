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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
)

const (
	GeneralKey = "GENERAL"
	MetricsKey = "METRICS"
	RISCVKey   = "RISCV"
	SNTMCIKey  = "SNT_MCI"

	MetricGeneralKey = "general"
	RISCVDoomKey     = "DOOM"
	SNTMCIDCRKey     = "DCR"
	DCRMCICTRLKey    = "MCI_CTRL"
	BusIdKey         = "sen_bus_id"
	EnableKey        = "enable"
	PathKey          = "path"
	EnableRISCVKey   = "ENABLE_RISCV"

	TemplatePathKey = "SENLIB_CONFIG_TEMPLATE_FILEPATH"

	resourcePoolFileName = "resource_pool"
	defaultTemplatePath  = "/etc/senlib-config-template"

	WildCardLocalMetricPath = "metrics.%BUSID"
	UnknownDevice           = "unknown"
)

var (
	NoGeneralKeyErr = fmt.Errorf("cannot find %s config", GeneralKey)
)

type SenlibConfigGeneral struct {
	PciAddresses         []string `json:"sen_bus_id"`
	MultiSpyreConfigPath string   `json:"multi_aiu_config_path"`
	Doom                 bool     `json:"doom"`
}

type SenlibConfigMetricGeneral struct {
	General SenlibConfigMetric `json:"general"`
}

type SenlibConfigMetric struct {
	Enable bool   `json:"enable"`
	Path   string `json:"path"`
}

type SenlibConfig struct {
	General SenlibConfigGeneral       `json:"GENERAL"`
	Metric  SenlibConfigMetricGeneral `json:"METRICS"`
}

type SenlibConfigGenerator struct {
	templateFilePath string
}

func NewSenlibConfigGenerator() SenlibConfigGenerator {
	templatePath := utils.GetEnvOrDefault(TemplatePathKey, defaultTemplatePath)
	templateFilePath := fmt.Sprintf("%s/%s", templatePath, GetConfigFileName())

	return SenlibConfigGenerator{
		templateFilePath: templateFilePath,
	}
}

/*
GenerateConfigContent generates config json file based on senlib config template file and allocated bus ids by
 1. adding bus ids to `GENERAL.sen_bus_id`.
 2. setting `METRICS.path` to a file/folder location for a process to write metrics of single/multiple AIU(s)
    2.1. For `METRICS.enable: true`, set path to /tmp/spyre-metrics/metrics.%BUSID
    2.2. Otherwise, set to metrics.%BUSID (default value)
*/
func (g SenlibConfigGenerator) GenerateConfigContent(resourcePool string, busIds []string, metricsPath string) (content []byte, err error) {
	// Open the JSON file
	var file []byte
	file, err = os.ReadFile(g.templateFilePath)
	if err != nil {
		return content, fmt.Errorf("error opening file: %v", err)
	}

	var configMap map[string]any

	// Unmarshal the JSON data
	if err = json.Unmarshal(file, &configMap); err == nil {
		if generalConfigInterface, found := configMap[GeneralKey]; found {
			if _, ok := generalConfigInterface.(map[string]any); ok {
				// set .GENERAL fields
				configMap[GeneralKey].(map[string]any)[BusIdKey] = busIds
				switch {
				case strings.Contains(resourcePool, "spyre_pf"):
					configMap[GeneralKey].(map[string]any)["doom"] = false
				default:
					configMap[GeneralKey].(map[string]any)["doom"] = true
				}
				// set .METRICS.general by checking .METRICS.general.enable
				if metricsConfigInterface, found := configMap[MetricsKey]; found {
					var metricsConfig map[string]any
					if metricsConfig, ok = metricsConfigInterface.(map[string]any); ok {
						if metricsGeneralConfigInterface, found := metricsConfig[MetricGeneralKey]; found {
							if metricGeneralConfig, ok := metricsGeneralConfigInterface.(map[string]any); ok {
								if enableInterface, found := metricGeneralConfig[EnableKey]; found {
									metricEnabled := false
									if metricEnabled, ok = enableInterface.(bool); !ok {
										metricEnabled = false
									}
									if metricEnabled {
										configMap[MetricsKey].(map[string]any)[MetricGeneralKey].(map[string]any)[PathKey] = filepath.Join(metricsPath, WildCardLocalMetricPath) //nolint:lll
									} else {
										configMap[MetricsKey].(map[string]any)[MetricGeneralKey].(map[string]any)[PathKey] = WildCardLocalMetricPath //nolint:lll
									}
								}
							} else {
								err = fmt.Errorf("failed to parse METRICS.general: %v", metricsGeneralConfigInterface)
							}
						} else {
							// general key not found
							configMap[MetricsKey].(map[string]any)[MetricGeneralKey] = map[string]any{
								EnableKey: false,
								PathKey:   WildCardLocalMetricPath,
							}
						}
					} else {
						err = fmt.Errorf("failed to parse METRICS: %v", metricsConfigInterface)
					}
				} else {
					// METRICS key not found
					configMap[MetricsKey] = map[string]any{
						MetricGeneralKey: map[string]any{
							EnableKey: false,
							PathKey:   WildCardLocalMetricPath,
						},
					}
				}
				configMap = modifyRISCVContent(configMap, resourcePool)
				if err == nil {
					content, err = json.Marshal(configMap)
				}
			} else {
				err = fmt.Errorf("failed to parse GENERAL: %v", generalConfigInterface)
			}
		} else {
			err = NoGeneralKeyErr
		}
	} else {
		err = fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return content, err
}

func (g SenlibConfigGenerator) GenerateConfigFile(resourcePool string, busIds []string, outputPath string) error {
	if err := g.writeResourcePoolFile(resourcePool, outputPath); err != nil {
		return err
	}
	content, err := g.GenerateConfigContent(resourcePool, busIds, GetMetricsContainerPath())
	if err != nil {
		return err
	}
	outputFilePath := filepath.Join(outputPath, GetConfigFileName())
	file, err := os.Create(outputFilePath)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck
	_, err = file.Write(content)
	return err
}

func (g SenlibConfigGenerator) writeResourcePoolFile(resourcePool string, outputPath string) error {
	outputFilePath := filepath.Join(outputPath, resourcePoolFileName)
	file, err := os.Create(outputFilePath)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck
	_, err = file.WriteString(resourcePool)
	glog.Infof("write %s to %s", resourcePool, outputFilePath)
	return err
}

func ReadSenlibConfig(mntPath string) ([]string, error) {
	senlibFilepath := filepath.Join(mntPath, GetConfigFileName())
	file, err := os.Open(senlibFilepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close() //nolint:errcheck
	var config SenlibConfig
	err = json.NewDecoder(file).Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %v", err)
	}
	return config.General.PciAddresses, nil
}

func ReadResourcePool(mntPath string) (string, error) {
	senlibFilepath := filepath.Join(mntPath, resourcePoolFileName)
	file, err := os.Open(senlibFilepath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close() //nolint:errcheck
	data, err := io.ReadAll(file)
	return string(data), err
}

// modifyRISCVContent applies the following config if resourcePool doesn't contain spyre_vf (PF Mode).
//
//	{
//	  "SNT_MCI": {
//	    "DCR": {
//	      "MCI_CTRL": {
//	        "ENABLE_RISCV": "0x0"
//	      }
//	    }
//	  }
//	}
func modifyRISCVContent(configMap map[string]any, resourcePool string) map[string]any {
	if strings.Contains(resourcePool, "spyre_vf") {
		// no modification needed, VF mode is default.
		return configMap
	}
	// PF mode
	if _, ok := configMap[SNTMCIKey]; !ok {
		configMap[SNTMCIKey] = make(map[string]any)
	}
	if _, ok := configMap[SNTMCIKey].(map[string]any)[SNTMCIDCRKey]; !ok {
		configMap[SNTMCIKey].(map[string]any)[SNTMCIDCRKey] = make(map[string]any)
	}
	if _, ok := configMap[SNTMCIKey].(map[string]any)[SNTMCIDCRKey].(map[string]any)[DCRMCICTRLKey]; !ok {
		configMap[SNTMCIKey].(map[string]any)[SNTMCIDCRKey].(map[string]any)[DCRMCICTRLKey] = make(map[string]any)
	}
	configMap[SNTMCIKey].(map[string]any)[SNTMCIDCRKey].(map[string]any)[DCRMCICTRLKey].(map[string]any)[EnableRISCVKey] = "0x0" //nolint:lll
	return configMap
}
