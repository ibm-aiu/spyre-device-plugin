/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"context"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/factory"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	"github.com/jaypipes/ghw/pkg/pci"
)

var (
	lastUpdateTimeMutex sync.RWMutex
)

func NewUnhealthyDeviceState(deviceType pb.DEVICE_TYPE, state pb.DEVICE_STATE) *DeviceHealthState {
	health := NewDeviceHealthState(deviceType)
	health.SetHealthState(state)
	return health
}

func NewTestDeviceProvider() types.DeviceProvider {
	rf := factory.NewResourceFactory("ibm.com", "sock", true, "", nil)
	return spyredevice.NewSpyreDeviceProvider(rf)
}

// NewTestHealthInfoHandler returns health handler which has no client configured
func NewTestHealthInfoHandler() *HealthInfoHandler {
	deviceProvider := NewTestDeviceProvider()
	checker := NewPCIMonitor(DefaultScanInterval)
	return &HealthInfoHandler{
		deviceProvider:   deviceProvider,
		checker:          checker,
		updateChan:       make(chan struct{}, 1),
		debounceInterval: DefaultDebounceInterval,
		stopChan:         make(chan struct{}),
	}
}

// StartTestMode mimics processUpdate with sleep 10s.
func (h *HealthInfoHandler) StartTestMode() {
	go func() {
		for {
			select {
			case <-h.updateChan:
				if time.Since(h.lastUpdateTime) < h.debounceInterval {
					time.AfterFunc(h.debounceInterval, func() { SafeTriggerUpdate(h.updateChan) })
					continue
				}
				time.Sleep(10 * time.Second)
				lastUpdateTimeMutex.Lock()
				h.lastUpdateTime = time.Now()
				lastUpdateTimeMutex.Unlock()
				glog.V(1).Infof("Last update at %v", h.lastUpdateTime)
			case <-h.stopChan:
				return
			}
		}
	}()
}

func (h *HealthInfoHandler) GetDiscoveredDevices() []*pci.Device {
	return h.deviceProvider.GetDiscoveredDevices()
}

func (h *HealthInfoHandler) StopChan() chan struct{} {
	return h.stopChan
}

func (h *HealthInfoHandler) UpdateChan() chan struct{} {
	return h.updateChan
}

func (h *HealthInfoHandler) RediscoverDevices() error {
	return h.rediscoverDevices()
}

func (h *HealthInfoHandler) RediscoverAndGetDeviceInfo(ctx context.Context) ([]types.PciDevice, error) {
	return h.rediscoverAndGetDeviceInfo(ctx)
}

func InitHealthInfo(allDevices []types.PciDevice) map[string]DeviceHealthState {
	return initHealthInfo(allDevices)
}

func (h *HealthInfoHandler) GetLastUpdate() time.Time {
	lastUpdateTimeMutex.RLock()
	defer lastUpdateTimeMutex.RUnlock()
	return h.lastUpdateTime
}

func (t *SpyreHealthClient) SetHealths(healthInfoMap map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if healthInfoMap != nil {
		t.healthInfoMap = healthInfoMap
	}
}

// SetPCIDevicesPath is exported for testing
func SetPCIDevicesPath(path string) {
	setPCIDevicesPath(path)
}

var (
	SpyreHealthSocket  = spyreHealthSocket
	LoadTLSCredentials = loadTLSCredentials // pragma: allowlist secret
)
