/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"context"
	"os"
	"time"

	"github.com/golang/glog"

	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
)

type HealthChecker interface {
	// Start will be called after handler start processing updateChan
	Start(ctx context.Context, updateChan chan struct{}, initialDevices *pb.Devices) error
	// Stop will be called before handler close updateChan
	Stop()

	// UpdateHealths updates initialized health info list with latest state
	UpdateHealths(map[string]DeviceHealthState)
}

// GetHealthChecker returns SpyreHealthClient if socket exists,
// otherwise return PciMonitor with given scanInterval.
// Always connects to health checker with TLS enabled.
func GetHealthChecker(scanInterval time.Duration) HealthChecker {
	healthCheckerClient, err := NewSpyreHealthClient()
	if err == nil {
		glog.Info("Use SpyreHealthClient health checker")
		return healthCheckerClient
	}
	glog.Warning("Unable to get checker client", "err", err)
	if os.Getenv(spyrev1alpha1.PseudoDeviceMode.EnvKey()) == spyreconst.ModeEnabledValue {
		glog.Info("PseudoDeviceMode enabled, do not use PCIMonitor")
		return nil
	}
	glog.Info("Use PciMonitor health checker")
	return NewPCIMonitor(scanInterval)
}
