package dma

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
)

const (
	// P2PDMAEnvKey will be moved to operator const package later
	P2PDMAEnvKey = "P2PDMA"
)

// NeedP2PDMAConfigure returns true only if all following conditions are satisfied
// - deviceList has length more than 1
// - not in pseudo mode
// - P2PDMA is set to "1"
func NeedP2PDMAConfigure(deviceList []string) bool {
	if len(deviceList) <= 1 {
		return false
	}
	return !utils.IsPseudoDeviceMode() && os.Getenv(P2PDMAEnvKey) == spyreconst.ModeEnabledValue
}

func SetDevResourceFilePermissions(deviceIDs []string) error {
	for index, deviceID := range deviceIDs {
		if err := setDevResourceFilePermission(deviceID); err != nil {
			msg := fmt.Sprintf("failed to set resource file permission for P2P DMA on %s: %v", deviceID, err)
			// undo the previous steps
			if errs := UnsetDevResourceFilePermissions(deviceIDs[0:index]); len(errs) > 0 {
				msg += fmt.Sprintf(" and failed to undo previous steps: %v", errs)
			}
			return errors.New(msg)
		}
	}
	return nil
}

func UnsetDevResourceFilePermissions(deviceIDs []string) []error {
	var errs []error
	for _, deviceID := range deviceIDs {
		if err := unsetDevResourceFilePermission(deviceID); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// setDevResourceFilePermission sets /sys/bus/pci/devices/<dev>/resource2 to 0666 mode.
func setDevResourceFilePermission(dev string) error {
	resourceFile := filepath.Join(utils.SysBusPci, dev, "resource2")
	if !utils.PathExists(resourceFile) {
		return fmt.Errorf("%s not exists", resourceFile)
	}
	return os.Chmod(resourceFile, 0666)
}

// unsetDevResourceFilePermission sets /sys/bus/pci/devices/<dev>/resource2 back to 0600 mode.
func unsetDevResourceFilePermission(dev string) error {
	resourceFile := filepath.Join(utils.SysBusPci, dev, "resource2")
	if !utils.PathExists(resourceFile) {
		return fmt.Errorf("%s not exists", resourceFile)
	}
	return os.Chmod(resourceFile, 0600)
}
