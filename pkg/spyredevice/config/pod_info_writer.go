/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package config

import (
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	corev1 "k8s.io/api/core/v1"
)

const (
	PodNameFile      = "POD_NAME"
	PodNamespaceFile = "POD_NAMESPACE"
)

func writeInfoFiles(configFolder string, pod corev1.Pod) error {
	// write pod name
	if err := utils.WriteFile(configFolder, PodNameFile, pod.Name); err != nil {
		return err
	}
	// write pod namespace
	return utils.WriteFile(configFolder, PodNamespaceFile, pod.Namespace)
}
