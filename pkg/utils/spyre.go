/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package utils

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	ResourceNamePrefix = "ibm.com"
)

func GetPseudoVfAddress(pfAddress string, vfIndex int) string {
	if vfIndex < 1 {
		return ""
	}
	splits := strings.Split(pfAddress, ".")
	return fmt.Sprintf("%s.%d", splits[0], vfIndex)
}

func GetPseudoPfAddress(vfAddress string) string {
	splits := strings.Split(vfAddress, ".")
	if (len(splits) == 2 && splits[1] == "0") || len(splits) != 2 {
		return ""
	}
	return fmt.Sprintf("%s.0", splits[0])
}

func GetResourceNameFromPod(p *corev1.Pod) string {
	for _, container := range p.Spec.Containers {
		for resource := range container.Resources.Requests {
			fullResourceName := resource.String()
			if strings.Contains(fullResourceName, ResourceNamePrefix) {
				resourceName := strings.Replace(fullResourceName, ResourceNamePrefix, "", 1)
				return resourceName[1:] // resourceName from slash
			}
		}
	}
	return ""
}
