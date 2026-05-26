/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	"k8s.io/client-go/rest"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type HealthInfoHandler struct {
	deviceProvider    types.DeviceProvider
	checker           HealthChecker
	spyreClient       *spyreclient.SpyreClient
	cfg               *rest.Config
	resourceServers   []types.ResourceServer
	newDeviceDetected func(context.Context, []types.PciDevice)

	lastUpdateTime   time.Time
	debounceInterval time.Duration
	updateChan       chan struct{}
	stopChan         chan struct{}
}

func NewHealthInfoHandler(
	deviceProvider types.DeviceProvider,
	checker HealthChecker,
	cfg *rest.Config,
	spyreClient *spyreclient.SpyreClient,
	debounceInterval time.Duration,
	newDeviceDetected func(context.Context, []types.PciDevice),
	resourceServers ...types.ResourceServer) (*HealthInfoHandler, error) {

	if deviceProvider == nil {
		glog.Errorf("DeviceProvider cannot be nil")
		return nil, fmt.Errorf("DeviceProvider cannot be nil")
	}
	if cfg == nil {
		glog.Errorf("rest.Config cannot be nil")
		return nil, fmt.Errorf("rest.Config cannot be nil")
	}
	if spyreClient == nil {
		glog.Errorf("spyreClient cannot be nil")
		return nil, fmt.Errorf("spyreClient cannot be nil")
	}
	if debounceInterval <= 0 {
		debounceInterval = DefaultDebounceInterval
	}

	return &HealthInfoHandler{
		checker:           checker,
		deviceProvider:    deviceProvider,
		spyreClient:       spyreClient,
		cfg:               cfg,
		resourceServers:   resourceServers,
		debounceInterval:  debounceInterval,
		newDeviceDetected: newDeviceDetected,
		updateChan:        make(chan struct{}, 1),
		stopChan:          make(chan struct{}),
	}, nil
}

func (h *HealthInfoHandler) Start(ctx context.Context, pciDevices []types.PciDevice) error {
	// Ensure dynamic topology exists at startup (if source of truth available)
	if err := spyretopo.EnsureDynamicTopologyFiltered(); err != nil {
		glog.V(1).Infof("Ensure dynamic topology filtered: %v", err)
	}

	// convert to pb.Devices
	pbDevices := make([]*pb.Device, len(pciDevices))
	for _, dev := range pciDevices {
		pbDevice := &pb.Device{
			DeviceID: &pb.DeviceID{
				PCIAddress: dev.GetPciAddr(),
			},
			DeviceState: pb.DEVICE_STATE_ONLINE,
		}
		switch dev.GetDeviceCode() {
		case resources.PfProductId:
			pbDevice.DeviceState = pb.DEVICE_STATE(pb.DEVICE_TYPE_PF)
		case resources.VfProductId:
			pbDevice.DeviceState = pb.DEVICE_STATE(pb.DEVICE_TYPE_VF)
		}
		pbDevices = append(pbDevices, pbDevice)
	}

	go h.processUpdate(ctx)
	if err := h.checker.Start(ctx, h.updateChan, &pb.Devices{
		Devices: pbDevices,
	}); err != nil {
		h.stopChan <- struct{}{}
		return fmt.Errorf("failed to start health checker: %w", err)
	}
	return nil
}

func (h *HealthInfoHandler) Stop() {
	h.checker.Stop()
	close(h.stopChan)
	glog.Info("Health handler stopped")
}

func (h *HealthInfoHandler) GetDebounceInterval() time.Duration {
	return h.debounceInterval
}

func (h *HealthInfoHandler) processUpdate(ctx context.Context) {
	for {
		select {
		case <-h.updateChan:
			if time.Since(h.lastUpdateTime) < h.debounceInterval {
				time.AfterFunc(h.debounceInterval, func() { SafeTriggerUpdate(h.updateChan) })
				continue
			}
			h.updateSpyreNodestate(ctx)
			h.lastUpdateTime = time.Now()
			glog.V(1).Infof("Last update at %v", h.lastUpdateTime)
		case <-h.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// rediscoverAndGetDeviceInfo rediscovers devices (updates DeviceProvider)
// returns PciDevice info as a list.
func (h *HealthInfoHandler) rediscoverAndGetDeviceInfo(_ context.Context) ([]types.PciDevice, error) {
	if err := h.rediscoverDevices(); err != nil {
		glog.Errorf("Failed to rediscover devices: %v", err)
		return nil, err
	}

	rc := &types.ResourceConfig{
		DeviceType: types.SpyreDeviceType,
	}
	return h.deviceProvider.GetDevices(rc), nil
}

func (h *HealthInfoHandler) updateSpyreNodestate(ctx context.Context) {
	allDevices, err := h.rediscoverAndGetDeviceInfo(ctx)
	if err != nil {
		return
	}
	glog.Infof("Current device inventory:")
	glog.Infof("\tFound %d devices properly bound in filesystem", len(allDevices))

	healthInfoMap := initHealthInfo(allDevices)
	h.checker.UpdateHealths(healthInfoMap)

	existingDeviceAddresses := h.GetExistingDeviceAddresses()
	newDevices, changed := h.IdentifyDeviceChanges(allDevices, existingDeviceAddresses, healthInfoMap)

	hasNewDevices := len(newDevices) > 0
	if hasNewDevices {
		glog.Infof("Found %d new devices - triggering update", len(newDevices))
		if h.newDeviceDetected != nil {
			glog.Infof("Calling ResourceManager to update servers SpyreNodeState")
			h.newDeviceDetected(ctx, newDevices)
		} else {
			glog.Warningf("No callback registered for handling new devices. Trigger existing servers")
			// Trigger ListAndWatch for all resource servers
			h.UpdateResourceServers(newDevices)
		}
	}

	// Process device health status
	uniqueDevices, unhealthyDevices := h.ProcessDeviceHealth(allDevices, healthInfoMap)

	// Update dynamic topology based on current device health
	if changed {
		h.updateDynamicTopology(uniqueDevices)
	}

	// Write devices to SpyreNodeState
	h.writeDevicesToNodeState(ctx, uniqueDevices, unhealthyDevices)
}

// updateDynamicTopology filters the original topology based on current device health
// and saves the filtered topology to the dynamic location for immediate use.
func (h *HealthInfoHandler) updateDynamicTopology(devices map[string]types.PciDevice) {
	originalTopoFile := spyretopo.GetOriginalTopologyFile()
	if originalTopoFile == "" {
		glog.V(1).Info("No original topology file available, skipping dynamic topology update")
		return
	}

	data, err := os.ReadFile(originalTopoFile)
	if err != nil {
		glog.Warningf("Failed to read original topology file %s: %v", originalTopoFile, err)
		return
	}

	originalTopo, err := pcitopov2.UnmarshalPciTopo(data)
	if err != nil {
		glog.Warningf("Failed to unmarshal original topology: %v", err)
		return
	}

	healthyDevices := make(map[string]bool)
	for pciAddr, device := range devices {
		healthyDevices[pciAddr] = (device.GetHealth() == pluginapi.Healthy)
	}

	filteredTopo := spyretopo.FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
	if err := spyretopo.SaveDynamicTopology(filteredTopo); err != nil {
		glog.Warningf("Failed to save dynamic topology: %v", err)
		return
	}
	spyretopo.PciTopology = nil
	glog.V(1).Infof("Updated dynamic topology with %d devices and %d VF devices",
		filteredTopo.NumDevices, filteredTopo.SpyreVfNumDevices)
}

func (h *HealthInfoHandler) writeDevicesToNodeState(ctx context.Context,
	uniqueDevices map[string]types.PciDevice, unhealthyDevices []spyrev1alpha1.UnhealthyDevice) {
	uniqueDeviceSlice := make([]types.PciDevice, 0, len(uniqueDevices))
	for _, device := range uniqueDevices {
		uniqueDeviceSlice = append(uniqueDeviceSlice, device)
	}

	// This prevents redundant topology updates and ensures only pci_watcher controls topology
	_, err := spyredevice.WriteSpyreInterfacesToNodeState(ctx, h.cfg, uniqueDeviceSlice, h.spyreClient, false, unhealthyDevices)
	if err != nil {
		glog.Errorf("Failed to update SpyreNodeState: %v", err)
	}
}

// rediscoverDevices updates the DeviceProvider with any newly added devices
func (h *HealthInfoHandler) rediscoverDevices() error {
	devices, err := GetAllPCIDevices()
	if err != nil {
		glog.Errorf("Failed to get all PCI devices: %v", err)
		return err
	}

	deviceCodes := types.SupportedDevices[types.SpyreDeviceType]

	if err := h.deviceProvider.AddTargetDevices(devices, deviceCodes); err != nil {
		glog.Errorf("Failed to add target devices: %v", err)
		return err
	}

	return nil
}

func (h *HealthInfoHandler) UpdateResourceServers(newDevices []types.PciDevice) {
	for _, rs := range h.resourceServers {
		if rs == nil {
			continue
		}
		isPf, isVf := ContainsVForPF(newDevices)
		resourceName := rs.GetResourcePool().GetResourceName()
		if (isPf && strings.HasSuffix(resourceName, "pf")) || (isVf && strings.HasSuffix(resourceName, "vf")) {
			rs.TriggerUpdate()
		}
	}
}

func (h *HealthInfoHandler) GetExistingDeviceAddresses() map[string]bool {
	existingDeviceAddresses := make(map[string]bool)
	for _, rs := range h.resourceServers {
		if rs != nil {
			deviceMap := rs.GetResourcePool().GetDevices()
			for deviceID := range deviceMap {
				existingDeviceAddresses[deviceID] = true
			}
		}
	}
	return existingDeviceAddresses
}

func (h *HealthInfoHandler) IdentifyDeviceChanges(
	allDevices []types.PciDevice,
	existingDeviceAddresses map[string]bool,
	healthInfoMap map[string]DeviceHealthState,
) ([]types.PciDevice, bool) {
	newDevices := make([]types.PciDevice, 0)
	deviceChanges := false

	for _, device := range allDevices {
		pciAddr := device.GetPciAddr()
		if _, exists := existingDeviceAddresses[pciAddr]; !exists {
			glog.Infof("New device %s needs to be added to resource pools", pciAddr)
			newDevices = append(newDevices, device)
			deviceChanges = true
		} else if healthInfo, found := healthInfoMap[pciAddr]; found { // exists and has health info
			lastHealthy := device.GetHealth() == pluginapi.Healthy
			newHealthy := healthInfo.Healthy()
			if newHealthy != lastHealthy {
				glog.Infof("Device %s healthy change from %v to %v (%s)",
					pciAddr, lastHealthy, newHealthy, healthInfo.GetHealthState())
				deviceChanges = true
			}
		}
	}

	for addr := range existingDeviceAddresses {
		if _, exists := healthInfoMap[addr]; !exists {
			glog.Infof("Device %s now missing", addr)
			deviceChanges = true
		}
	}

	return newDevices, deviceChanges
}

// ProcessDeviceHealth sets health of *pluginapi.Device and returns
// a list of unique devices and unhealthy device states.
func (h *HealthInfoHandler) ProcessDeviceHealth(
	allDevices []types.PciDevice,
	healthInfoMap map[string]DeviceHealthState,
) (map[string]types.PciDevice, []spyrev1alpha1.UnhealthyDevice) {
	healthyDevices := make(map[string]struct{}, 0)
	unhealthyDevices := []spyrev1alpha1.UnhealthyDevice{}
	uniqueDevices := make(map[string]types.PciDevice)
	for _, device := range allDevices {
		pciAddr := device.GetPciAddr()
		uniqueDevices[pciAddr] = device
	}

	for pciAddr, device := range uniqueDevices {
		healthInfo, exists := healthInfoMap[pciAddr]
		if !exists {
			// no info, skip health update
			continue
		}

		switch healthInfo.GetHealthState() {
		case pb.DEVICE_STATE_ONLINE:
			device.SetHealth(pluginapi.Healthy)
			healthyDevices[pciAddr] = struct{}{}
		// Potential extension: auto-recovery based on health state
		default:
			device.SetHealth(pluginapi.Unhealthy)
			unhealthyDevices = append(unhealthyDevices,
				spyrev1alpha1.UnhealthyDevice{
					ID:    pciAddr,
					State: healthInfo.GetHealthState().String(),
				},
			)
		}

	}

	// ensure VF inherit unhealthy from PF
	inheritedUnhealthyVFs := []string{}
	// // check VFs in healthyDevices map
	for pciAddr := range healthyDevices {
		if device, found := uniqueDevices[pciAddr]; found {
			if device.IsIsolatedVF() || device.IsSriovPF() {
				continue
			}
			pfPciaddr := device.GetPfPciAddr()
			if healthInfo, found := healthInfoMap[pfPciaddr]; found &&
				healthInfo.deviceType == pb.DEVICE_TYPE_PF {
				if !healthInfo.Healthy() {
					device.SetHealth(pluginapi.Unhealthy)
					inheritedUnhealthyVFs = append(inheritedUnhealthyVFs, pciAddr)
					unhealthyDevices = append(unhealthyDevices,
						spyrev1alpha1.UnhealthyDevice{
							ID:    pciAddr,
							State: healthInfo.GetHealthState().String(),
						},
					)
				}
			} else {
				glog.Infof("cannot find PF health of %s", pfPciaddr)
			}
		}
		// otherwise, ignore as it will be treated as missing later on SpyreNodeState update
	}
	if len(inheritedUnhealthyVFs) > 0 {
		glog.Infof("Inherited unhealthy VF devices (%d):", len(inheritedUnhealthyVFs))
		// // remove inherited unhealthy devices from healthy map
		for _, addr := range inheritedUnhealthyVFs {
			delete(healthyDevices, addr)
			glog.Infof("\t- %s set to unhealthy according to its PF", addr)
		}
	}

	if len(healthyDevices) > 0 {
		glog.Infof("Healthy devices (%d):", len(healthyDevices))
		for addr := range healthyDevices {
			glog.Infof("\t- %s: bound to vfio-pci", addr)
		}
	}

	if len(unhealthyDevices) > 0 {
		glog.Infof("Unhealthy devices (%d):", len(unhealthyDevices))
		for _, dev := range unhealthyDevices {
			glog.Infof("\t- %s: reported as %s", dev.ID, dev.State)
		}
	}

	return uniqueDevices, unhealthyDevices
}

// initHealthInfo initialize health info for all devices as healthy.
func initHealthInfo(allDevices []types.PciDevice) map[string]DeviceHealthState {
	healthInfoMap := make(map[string]DeviceHealthState)
	for _, device := range allDevices {
		addr := device.GetPciAddr()
		if device.IsSriovPF() {
			healthInfoMap[addr] = *NewDeviceHealthState(pb.DEVICE_TYPE_PF)
		} else {
			healthInfoMap[addr] = *NewDeviceHealthState(pb.DEVICE_TYPE_VF)
		}
	}
	return healthInfoMap
}

func SafeTriggerUpdate(updateChan chan struct{}) {
	select {
	case updateChan <- struct{}{}:
		glog.V(1).Info("Update triggered")
	default:
		glog.V(1).Info("Update already pending, skipping")
	}
}
