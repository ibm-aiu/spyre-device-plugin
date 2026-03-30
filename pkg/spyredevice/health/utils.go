/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"os"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
)

func ContainsVForPF(newDevices []types.PciDevice) (bool, bool) {
	var isPf, isVf bool
	for _, dev := range newDevices {
		if isVf && isPf {
			break
		}
		if dev.IsSriovPF() {
			isPf = true
		} else {
			isVf = true
		}
	}
	return isPf, isVf
}

func MapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, a_val := range a {
		b_val, ok := b[k]
		if !ok || a_val != b_val {
			return false
		}
	}
	return true
}

func SafeRemove(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
