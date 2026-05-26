/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
//
// Copyright 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/golang/glog"
	"github.com/jaypipes/ghw"

	"k8s.io/client-go/rest"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/factory"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	spyrehealth "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/health"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	"github.com/ibm-aiu/spyre-operator/controllers/spyrepod"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
)

const (
	socketSuffix     = "sock"
	sriovVFArch      = "s390x"
	ScanInterval     = 15 * time.Second
	DebounceInterval = 2 * time.Second
)

type CliParams struct {
	ConfigFile       string
	ResourcePrefix   string
	TopologyFilepath string
	ProbePort        string
}

type ResourceManager struct {
	CliParams
	pluginWatchMode    bool
	rFactory           types.ResourceFactory
	configList         []*types.ResourceConfig
	resourceServers    []types.ResourceServer
	deviceProviders    map[types.DeviceType]types.DeviceProvider
	healthInfoHandler  *spyrehealth.HealthInfoHandler
	filteredDevices    []types.PciDevice
	probeManager       ctrl.Manager
	probeManagerCtx    context.Context
	probeManagerCancel context.CancelFunc
}

func NewResourceManager(cp *CliParams, spyreClient *spyreclient.SpyreClient) *ResourceManager {
	return NewResourceManagerWithConfig(cp, spyreClient, nil)
}

// NewResourceManagerWithConfig creates a new ResourceManager with an optional Kubernetes config.
// If cfg is nil, it will use ctrl.GetConfigOrDie() to get the config.
// This allows tests to provide a mock config from envtest.
func NewResourceManagerWithConfig(
	cp *CliParams,
	spyreClient *spyreclient.SpyreClient,
	cfg *rest.Config,
) *ResourceManager {
	pluginWatchMode := utils.DetectPluginWatchMode(types.SockDir)
	if pluginWatchMode {
		glog.Infof("Using Kubelet Plugin Registry Mode")
	} else {
		glog.Infof("Using Deprecated Device Plugin Registry Path")
	}
	rf := factory.NewResourceFactory(cp.ResourcePrefix, socketSuffix, pluginWatchMode, cp.TopologyFilepath, spyreClient)
	dp := make(map[types.DeviceType]types.DeviceProvider)
	for k := range types.SupportedDevices {
		dp[k] = rf.GetDeviceProvider(k)
	}

	ctx, cancel := context.WithCancel(context.Background())
	probePort := cp.ProbePort
	if probePort == "" {
		probePort = "8081"
	}
	probeAddress := ":" + probePort

	// Use provided config or get from environment
	if cfg == nil {
		cfg = ctrl.GetConfigOrDie()
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		HealthProbeBindAddress: probeAddress,
		LeaderElection:         false,
	})
	if err != nil {
		glog.Errorf("Could not create controller-runtime manager: %v", err)
		cancel()
		return nil
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		glog.Errorf("Could not add health check to manager: %v", err)
		cancel()
		return nil
	}

	if err := mgr.AddReadyzCheck("readyz", func(_ *http.Request) error {
		if _, err := os.Stat("/var/lib/kubelet/device-plugins/kubelet.sock"); os.IsNotExist(err) {
			glog.V(1).Infof("Readiness check: kubelet socket not found")
			return errors.New("kubelet socket not found")
		}
		return nil
	}); err != nil {
		glog.Errorf("Could not add readiness check to manager: %v", err)
		cancel()
		return nil
	}

	rm := &ResourceManager{
		CliParams:          *cp,
		pluginWatchMode:    pluginWatchMode,
		rFactory:           rf,
		deviceProviders:    dp,
		probeManagerCtx:    ctx,
		probeManagerCancel: cancel,
		probeManager:       mgr,
	}

	go func() {
		glog.V(1).Info("Starting probe server on port 8081")
		if err := mgr.Start(ctx); err != nil {
			glog.Errorf("Probe manager stopped with error: %v", err)
		}
	}()

	return rm
}

// ReadConfig reads and validate configurations from Config file
func (rm *ResourceManager) ReadConfig() error {
	resources := &types.ResourceConfList{}
	rawBytes, err := os.ReadFile(rm.ConfigFile)

	if err != nil {
		return fmt.Errorf("error reading file %s, %v", rm.ConfigFile, err)
	}

	glog.V(1).Infof("raw ResourceList: %s", rawBytes)
	if err = json.Unmarshal(rawBytes, resources); err != nil {
		glog.Errorf("failed to unmarshal config JSON data")
		return err
	}

	for i := range resources.ResourceList {
		conf := &resources.ResourceList[i]
		if os.Getenv(spyrev1alpha1.DisableVfMode.EnvKey()) == spyreconst.ModeEnabledValue &&
			conf.ResourceName == spyreconst.VfResourceName {
			continue
		}
		// Validate deviceType
		if conf.DeviceType == "" {
			conf.DeviceType = types.SpyreDeviceType // Default to SpyreDeviceType
		} else if _, ok := types.SupportedDevices[conf.DeviceType]; !ok {
			return fmt.Errorf("unsupported deviceType:  \"%s\"", conf.DeviceType)
		}
		if conf.SelectorObj, err = rm.rFactory.GetDeviceFilter(conf); err == nil {
			rm.configList = append(rm.configList, &resources.ResourceList[i])
		} else {
			glog.Warningf("unable to get SelectorObj from selectors list:'%s' for deviceType: %s error: %s",
				*conf.Selectors, conf.DeviceType, err)
		}
	}
	glog.V(1).Infof("unmarshaled ResourceList: %+v", resources.ResourceList)
	return nil
}

func createResourceServer(
	rf types.ResourceFactory,
	rc *types.ResourceConfig,
	devices []types.PciDevice,
) (types.ResourceServer, error) {
	rPool, err := rf.GetResourcePool(rc, devices)
	if err != nil {
		glog.Errorf("initServers(): error creating ResourcePool with config %+v: %q", rc, err)
		return nil, err
	}
	// Create ResourceServer with this ResourcePool
	s, err := rf.GetResourceServer(rPool)
	if err != nil {
		glog.Errorf("initServers(): error creating ResourceServer: %v", err)
		return nil, err
	}
	return s, nil
}

func (rm *ResourceManager) InitServers() error {
	rf := rm.rFactory
	glog.Infof("Initializing servers: %d configs\n", len(rm.configList))
	deviceAllocated := make(map[string]bool)

	for _, rc := range rm.configList {
		// Create new ResourcePool
		glog.Infof("Creating new ResourcePool: %s", rc.ResourceName)
		glog.V(1).Infof("DeviceType: %+v", rc.DeviceType)
		dp, ok := rm.deviceProviders[rc.DeviceType]
		if !ok {
			glog.Errorf("Unable to get device provider from deviceType: %s", rc.DeviceType)
			return fmt.Errorf("error getting device provider")
		}

		devices := dp.GetDevices(rc)
		glog.V(1).Infof("number of devices: %d\n", len(devices))
		filteredDevices, err := dp.GetFilteredDevices(devices, rc)
		if err != nil {
			glog.Errorf("initServers(): error getting filtered devices for config %+v: %q", rc, err)
		}
		glog.V(1).Infof("number of filteredDevices before excludeAllocatedDevices: %d\n", len(filteredDevices))
		filteredDevices = rm.excludeAllocatedDevices(filteredDevices, deviceAllocated)

		if len(filteredDevices) < 1 {
			glog.Infof("no devices in device pool, skipping creating resource server for %s", rc.ResourceName)
			continue
		}
		s, err := createResourceServer(rf, rc, filteredDevices)
		if err != nil {
			glog.Errorf("failed to create resourceServer: %s", err)
			continue
		}
		if os.Getenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey()) == spyreconst.ModeEnabledValue &&
			(rc.ResourceName == spyreconst.PfResourceName || rc.ResourceName == spyreconst.VfResourceName) {
			glog.Infof("Adding per-device resource servers for %s", rc.ResourceName)
			perDevicePools := make(map[string][]types.PciDevice, len(filteredDevices))
			for _, dev := range filteredDevices {
				var pciAddress string
				if rc.ResourceName == spyreconst.PfResourceName || (rc.ResourceName == spyreconst.VfResourceName &&
					dev.IsIsolatedVF()) {
					pciAddress = dev.GetPciAddr()
				} else {
					pciAddress = dev.GetPfPciAddr()
				}
				n := spyrepod.SafePciAddress(rc.ResourceName, pciAddress)
				glog.V(1).Infof("resourceName: %s", n)
				perDevicePools[n] = append(perDevicePools[n], dev)
			}
			for n, dl := range perDevicePools {
				cfg := &types.ResourceConfig{
					ResourcePrefix: rc.ResourcePrefix,
					ResourceName:   n,
					DeviceType:     rc.DeviceType,
					Selectors:      rc.Selectors,
					SelectorObj:    s,
				}
				s, err := createResourceServer(rf, cfg, dl)
				if err != nil {
					glog.Errorf("failed to create resourceServer for %s: %s", n, err)
					continue
				}
				rm.resourceServers = append(rm.resourceServers, s)
			}
		}
		if os.Getenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey()) == spyreconst.ModeEnabledValue {
			glog.Infof("Adding per-policy resource servers for %s device", rc.ResourceName)
			policyNames := []string{
				rc.ResourceName + spyreconst.TierZeroResourceNameSuffix,
				rc.ResourceName + spyreconst.TierOneResourceNameSuffix,
				rc.ResourceName + spyreconst.TierTwoResourceNameSuffix,
			}
			for _, pName := range policyNames {
				glog.V(1).Infof("resourceName: %s", pName)
				cfg := &types.ResourceConfig{
					ResourcePrefix: rc.ResourcePrefix,
					ResourceName:   pName,
					DeviceType:     rc.DeviceType,
					Selectors:      rc.Selectors,
					SelectorObj:    s,
				}
				s, err := createResourceServer(rf, cfg, filteredDevices)
				if err != nil {
					glog.Errorf("failed to create resourceServer for %s: %s", pName, err)
					continue
				}
				rm.resourceServers = append(rm.resourceServers, s)
			}
		}

		glog.Infof("New resource server is created for %s ResourcePool", rc.ResourceName)
		rm.resourceServers = append(rm.resourceServers, s)
		rm.filteredDevices = append(rm.filteredDevices, filteredDevices...)
	}
	return nil
}

// handles newly detected devices
func (rm *ResourceManager) HandleHotpluggedDevices(
	ctx context.Context,
	cfg *rest.Config,
	spyreClient *spyreclient.SpyreClient,
	newDevices []types.PciDevice,
) {
	glog.Infof(
		"Hotplug event: %d new devices detected, adding per-device servers and triggering resource updates",
		len(newDevices),
	)

	rm.filteredDevices = append(rm.filteredDevices, newDevices...)
	// Add new devices to the device provider first
	if err := rm.addNewDevicesToProvider(newDevices); err != nil {
		glog.Errorf("Failed to add new devices to provider: %v", err)
		return
	}
	// per-Device servers
	rm.addPerDeviceServersForNewDevices(newDevices)

	// Update aggregated resource pools with new devices
	rm.updateAggregatedResourcePools(newDevices)

	if rm.healthInfoHandler != nil {
		rm.healthInfoHandler.UpdateResourceServers(newDevices)
	}
}

// addNewDevicesToProvider adds new devices to the device provider without full rediscovery
func (rm *ResourceManager) addNewDevicesToProvider(newDevices []types.PciDevice) error {
	allDevices, err := spyrehealth.GetAllPCIDevices()
	if err != nil {
		return fmt.Errorf("failed to get all PCI devices: %v", err)
	}

	deviceMap := make(map[string]*ghw.PCIDevice)
	for _, device := range allDevices {
		deviceMap[device.Address] = device
	}

	ghwDevices := make([]*ghw.PCIDevice, 0, len(newDevices))
	for _, device := range newDevices {
		if ghwDevice, exists := deviceMap[device.GetPciAddr()]; exists {
			ghwDevices = append(ghwDevices, ghwDevice)
		}
	}

	if len(ghwDevices) == 0 {
		return fmt.Errorf("no valid GHW devices found from new devices")
	}

	dp := rm.deviceProviders[types.SpyreDeviceType]
	deviceCodes := types.SupportedDevices[types.SpyreDeviceType]
	if err := dp.AddTargetDevices(ghwDevices, deviceCodes); err != nil {
		return fmt.Errorf("failed to add target devices to provider: %v", err)
	}

	glog.Infof("Successfully added %d new devices to device provider", len(ghwDevices))
	return nil
}

func (rm *ResourceManager) addPerDeviceServersForNewDevices(newDevices []types.PciDevice) {
	// Only add per-device servers if perDevice mode is enabled
	if os.Getenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey()) != spyreconst.ModeEnabledValue {
		glog.V(1).Info("PerDevice mode not enabled, skipping per-device server creation")
		return
	}
	glog.Infof("Adding per-device servers for %d new devices", len(newDevices))
	rm.filteredDevices = append(rm.filteredDevices, newDevices...)
	for _, newDevice := range newDevices {
		var resourceName string
		if newDevice.IsSriovPF() {
			resourceName = spyreconst.PfResourceName
		} else {
			resourceName = spyreconst.VfResourceName
		}
		var pciAddress string
		if resourceName == spyreconst.PfResourceName || (resourceName == spyreconst.VfResourceName &&
			newDevice.IsIsolatedVF()) {
			pciAddress = newDevice.GetPciAddr()
		} else {
			pciAddress = newDevice.GetPfPciAddr()
		}
		perDeviceName := spyrepod.SafePciAddress(resourceName, pciAddress)
		cfg := &types.ResourceConfig{
			ResourcePrefix: rm.ResourcePrefix,
			ResourceName:   perDeviceName,
			DeviceType:     types.SpyreDeviceType,
		}
		s, err := createResourceServer(rm.rFactory, cfg, []types.PciDevice{newDevice})
		if err != nil {
			glog.Errorf("Failed to create per-device server for %s: %v", perDeviceName, err)
			continue
		}
		if err := s.Start(); err != nil {
			glog.Errorf("Failed to start per-device server for %s: %v", perDeviceName, err)
			continue
		}
		rm.resourceServers = append(rm.resourceServers, s)
		glog.Infof("Successfully added per-device server: %s", perDeviceName)
	}
}

// containsPfOrVfDevices checks if the new devices contain PF or VF devices
func containsPfOrVfDevices(newDevices []types.PciDevice) (bool, bool) {
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

// updateAggregatedResourcePools refreshes aggregated and per-device resource pools with updated device lists
func (rm *ResourceManager) updateAggregatedResourcePools(newDevices []types.PciDevice) {
	isPf, isVf := containsPfOrVfDevices(newDevices)
	perDeviceMode := os.Getenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey()) == spyreconst.ModeEnabledValue

	for i, rs := range rm.resourceServers {
		if rs == nil {
			continue
		}

		resourceName := rs.GetResourcePool().GetResourceName()
		var shouldUpdate bool
		var configName string

		switch {
		case resourceName == spyreconst.PfResourceName && isPf:
			shouldUpdate = true
			configName = spyreconst.PfResourceName
		case resourceName == spyreconst.VfResourceName && isVf:
			shouldUpdate = true
			configName = spyreconst.VfResourceName
		case perDeviceMode && strings.HasPrefix(resourceName, spyreconst.PfResourceName+"_") && isPf && !strings.HasSuffix(
			resourceName,
			"tier",
		):
			shouldUpdate = true
			configName = spyreconst.PfResourceName // base PF config
		case perDeviceMode && strings.HasPrefix(resourceName, spyreconst.VfResourceName+"_") && isVf && !strings.HasSuffix(
			resourceName,
			"tier",
		):
			shouldUpdate = true
			configName = spyreconst.VfResourceName // base VF config
		}

		if !shouldUpdate {
			continue
		}

		// Find the base PF/VF config
		var baseConfig *types.ResourceConfig
		for _, rc := range rm.configList {
			if rc.ResourceName == configName {
				baseConfig = rc
				break
			}
		}
		if baseConfig == nil {
			glog.Warningf("No base config found for %s", resourceName)
			continue
		}

		// Stop old resource server before recreating
		if err := rs.Stop(); err != nil {
			glog.Warningf("Failed to stop old resource server %s: %v", resourceName, err)
		}

		newConfig := *baseConfig
		newConfig.ResourceName = resourceName

		dp := rm.deviceProviders[types.SpyreDeviceType]
		allDevices := dp.GetDevices(baseConfig)

		// Handle per-device resource filtering
		if perDeviceMode &&
			(strings.HasPrefix(resourceName, spyreconst.PfResourceName+"_") ||
				strings.HasPrefix(resourceName, spyreconst.VfResourceName+"_")) {

			pciAddr := extractPCIAddr(resourceName)
			glog.V(1).Infof(
				"Per-device filtering for %s: extracted PCI addr '%s' from %d devices",
				resourceName,
				pciAddr,
				len(allDevices),
			)
			allDevices = filterDevicesByPCIAddr(allDevices, pciAddr)
			glog.V(1).Infof("After filtering for PCI addr '%s': %d devices remain", pciAddr, len(allDevices))
		}

		filteredDevices, err := dp.GetFilteredDevices(allDevices, &newConfig)
		if err != nil {
			glog.Errorf("Failed to get filtered devices for %s: %v", resourceName, err)
			continue
		}

		newServer, err := createResourceServer(rm.rFactory, &newConfig, filteredDevices)
		if err != nil {
			glog.Errorf("Failed to create updated resource server for %s: %v", resourceName, err)
			continue
		}

		if err := newServer.Start(); err != nil {
			glog.Errorf("Failed to start updated resource server for %s: %v", resourceName, err)
			continue
		}

		rm.resourceServers[i] = newServer
		glog.Infof("Successfully updated resource server: %s", resourceName)
	}
}

// extractPCIAddr extracts the PCI address from a per-device resource name.
func extractPCIAddr(resourceName string) string {
	var addr string
	switch {
	case strings.HasPrefix(resourceName, spyreconst.PfResourceName+"_"):
		addr = strings.TrimPrefix(resourceName, spyreconst.PfResourceName+"_")
	case strings.HasPrefix(resourceName, spyreconst.VfResourceName+"_"):
		addr = strings.TrimPrefix(resourceName, spyreconst.VfResourceName+"_")
	default:
		return ""
	}
	addr = strings.ReplaceAll(addr, "_", ":")
	return addr
}

// filterDevicesByPCIAddr returns devices matching a specific PCI address.
func filterDevicesByPCIAddr(devices []types.PciDevice, addr string) []types.PciDevice {
	var result []types.PciDevice
	glog.V(1).Infof("filterDevicesByPCIAddr: looking for addr '%s' among %d devices", addr, len(devices))
	for _, d := range devices {
		deviceAddr := d.GetPciAddr()
		pfAddr := d.GetPfPciAddr()
		glog.V(1).Infof("filterDevicesByPCIAddr: device addr='%s', pf addr='%s', target='%s'", deviceAddr, pfAddr, addr)
		var matchAddr string
		if !d.IsSriovPF() && !d.IsIsolatedVF() {
			matchAddr = pfAddr
		} else {
			matchAddr = deviceAddr
		}

		if matchAddr == addr {
			result = append(result, d)
			glog.V(1).Infof("filterDevicesByPCIAddr: found match for '%s' using %s", addr, matchAddr)
		}
	}
	glog.V(1).Infof("filterDevicesByPCIAddr: returning %d devices for addr '%s'", len(result), addr)
	return result
}

func (rm *ResourceManager) StartSpyreNodeStateUpdateTicker(
	ctx context.Context,
	cfg *rest.Config,
	spyreClient *spyreclient.SpyreClient,
	quit chan interface{},
) {
	// Clear existing node state before first write
	if nodeState, err := spyredevice.GetNodeStateForThisNode(ctx, spyreClient); err == nil {
		nodeState.Spec.SpyreInterfaces = []spyrev1alpha1.SpyreInterface{}
		nodeState.Spec.SpyreSSAInterfaces = []spyrev1alpha1.SpyreSSAInterface{}
		nodeState.Spec.Pcitopo = ""
		if _, err = spyreClient.Update(ctx, nodeState, true); err != nil {
			glog.Error("failed to clear SpyreNodeState", err)
		}
	}

	// first call
	if _, err := spyredevice.WriteSpyreInterfacesToNodeState(
		ctx, cfg, rm.filteredDevices, spyreClient, false, nil,
	); err != nil {
		glog.Error("failed to write SpyreNodeState", err)
	}
	if _, err := spyredevice.InitAllocationList(ctx, cfg, spyreClient); err != nil {
		glog.Error("failed to patch allocation status", err)
	}

	glog.Info("Health checker running with TLS enabled")
	checker := spyrehealth.GetHealthChecker(ScanInterval)
	if checker == nil {
		glog.Info("No health checker, skip health checking")
		return
	}
	healthInfoHandler, err := spyrehealth.NewHealthInfoHandler(
		rm.deviceProviders[types.SpyreDeviceType],
		checker,
		cfg,
		spyreClient,
		DebounceInterval,
		func(ctx context.Context, newDevices []types.PciDevice) {
			rm.HandleHotpluggedDevices(ctx, cfg, spyreClient, newDevices)
		},
		rm.resourceServers...)

	if err != nil {
		glog.Errorf("failed to create HealthInfoHandler: %v", err)
		return
	}

	if err := healthInfoHandler.Start(ctx, rm.filteredDevices); err != nil {
		glog.Errorf("failed to start HealthInfoHandler: %v", err)
		return
	}

	rm.healthInfoHandler = healthInfoHandler

	go func() {
		for range quit {
			if rm.healthInfoHandler != nil {
				rm.healthInfoHandler.Stop()
			}
		}
	}()
}

func (rm *ResourceManager) excludeAllocatedDevices(
	filteredDevices []types.PciDevice,
	deviceAllocated map[string]bool,
) []types.PciDevice {
	filteredDevicesTemp := []types.PciDevice{}
	for _, dev := range filteredDevices {
		if !deviceAllocated[dev.GetPciAddr()] {
			deviceAllocated[dev.GetPciAddr()] = true
			filteredDevicesTemp = append(filteredDevicesTemp, dev)
		} else {
			glog.Warningf("Cannot add PCI Address [%s]. Already allocated.", dev.GetPciAddr())
		}
	}
	return filteredDevicesTemp
}

func (rm *ResourceManager) StartAllServers() error {
	for _, rs := range rm.resourceServers {
		if err := rs.Start(); err != nil {
			return err
		}

		// start watcher
		if !rm.pluginWatchMode {
			go rs.Watch()
		}
	}
	return nil
}

func (rm *ResourceManager) StopDevicePluginServers() error {
	if rm == nil {
		glog.Warning("StopDevicePluginServers called on nil ResourceManager")
		return nil
	}
	glog.Info("stop device plugin servers")
	var err error
	for _, rs := range rm.resourceServers {
		resourceName := rs.GetResourcePool().GetResourceName()
		glog.V(1).Infof("stopping resource server for resource %s", resourceName)
		if stopErr := rs.Stop(); stopErr != nil {
			glog.Errorf("failed to stop resource server for resource %s: %s", resourceName, stopErr.Error())
			err = stopErr
		}
	}

	return err
}

func (rm *ResourceManager) StopAllServers() error {
	if rm == nil {
		glog.Warning("StopAllServers called on nil ResourceManager")
		return nil
	}
	glog.Info("stop all servers")
	rm.StopProbeManager()
	var err error
	for _, rs := range rm.resourceServers {
		resourceName := rs.GetResourcePool().GetResourceName()
		glog.V(1).Infof("stopping resource server for resource %s", resourceName)
		if stopErr := rs.Stop(); stopErr != nil {
			glog.Errorf("failed to stop resource server for resource %s: %s", resourceName, stopErr.Error())
			err = stopErr
		}
	}

	if rm.healthInfoHandler != nil {
		glog.V(1).Infof("stopping health handler")
		rm.healthInfoHandler.Stop()
	}

	return err
}

// StopProbeManager stops the controller-runtime manager and cancels its context
func (rm *ResourceManager) StopProbeManager() {
	if rm.probeManagerCancel != nil {
		glog.Info("stop probe server")
		rm.probeManagerCancel()
		rm.probeManagerCancel = nil
		rm.probeManager = nil
	}
}

// Validate configurations
func (rm *ResourceManager) ValidConfigs() bool {

	if len(rm.configList) < 1 {
		glog.Errorf("no resource configuration; exiting")
		return false
	}

	resourceNames := make(map[string]string) // resource names placeholder

	for _, conf := range rm.configList {
		// check if name contains acceptable characters
		if !utils.ValidResourceName(conf.ResourceName) {
			glog.Errorf("resource name \"%s\" contains invalid characters", conf.ResourceName)
			return false
		}

		// resourcePrefix might be overridden for a given resource pool
		resourcePrefix := rm.ResourcePrefix
		if conf.ResourcePrefix != "" {
			resourcePrefix = conf.ResourcePrefix
		}

		resourceName := resourcePrefix + "/" + conf.ResourceName

		glog.V(1).Infof("validating resource name \"%s\"", resourceName)

		// ensure that resource name is unique
		if _, exists := resourceNames[resourceName]; exists {
			// resource name already exist
			glog.Errorf("resource name \"%s\" already exists", resourceName)
			return false
		}

		// Check if the DeviceType is valid
		if _, ok := types.SupportedDevices[conf.DeviceType]; !ok {
			glog.Errorf("unsupported deviceType:  \"%s\" already exists", conf.DeviceType)
			return false
		}

		// Check DeviceType-specific configuration
		if !rm.deviceProviders[conf.DeviceType].ValidConfig(conf) {
			return false
		}

		resourceNames[resourceName] = resourceName
	}

	return true
}

func (rm *ResourceManager) DiscoverHostDevices() error {
	var devices []*ghw.PCIDevice
	if os.Getenv(spyrev1alpha1.PseudoDeviceMode.EnvKey()) == spyreconst.ModeEnabledValue {
		topo, err := spyretopo.GetPciTopology("", false)
		if err == nil {
			for _, device := range topo.GetDevices() {
				devices = append(devices,
					spyredevice.GeneratePseudoDevice(device, resources.PfProductId))
			}
			// add isolated VF devices
			if runtime.GOARCH == sriovVFArch {
				for pciAddr := range topo.SpyreVfDevices {
					devices = append(devices, spyredevice.GeneratePseudoDevice(pciAddr, resources.VfProductId))
				}
			}
		} else {
			glog.Warningf("cannot get PCI topology config: %v, use default pseudo devices", err)
			devices = []*ghw.PCIDevice{
				// spyre_pf devices
				spyredevice.GeneratePseudoDevice("0000:1a:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:1c:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:1d:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:1e:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:3d:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:3f:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:40:00.0", resources.PfProductId),
				spyredevice.GeneratePseudoDevice("0000:41:00.0", resources.PfProductId),
			}
			if runtime.GOARCH == sriovVFArch {
				isolatedVfs := []*ghw.PCIDevice{
					spyredevice.GeneratePseudoDevice("0001:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0002:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0003:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0004:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0005:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0006:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0007:00:00.0", resources.VfProductId),
					spyredevice.GeneratePseudoDevice("0008:00:00.0", resources.VfProductId),
				}
				devices = append(devices, isolatedVfs...)
			}
		}
		if runtime.GOARCH != sriovVFArch {
			vfDevices := make([]*ghw.PCIDevice, 0, 2*len(devices))
			for _, device := range devices {
				// spyre_vf devices
				vf1 := utils.GetPseudoVfAddress(device.Address, 1)
				vf2 := utils.GetPseudoVfAddress(device.Address, 2)
				vfDevices = append(vfDevices, spyredevice.GeneratePseudoDevice(vf1, resources.VfProductId))
				vfDevices = append(vfDevices, spyredevice.GeneratePseudoDevice(vf2, resources.VfProductId))
			}
			devices = append(devices, vfDevices...)
		}
	} else {
		pci, err := ghw.PCI()
		if err != nil {
			return fmt.Errorf("discoverDevices(): error getting PCI info: %v", err)
		}

		devices = pci.Devices
		if len(devices) == 0 {
			glog.Warningf("discoverDevices(): no PCI device found")
		}
	}

	for k, v := range types.SupportedDevices {
		if dp, ok := rm.deviceProviders[k]; ok {
			if err := dp.AddTargetDevices(devices, v); err != nil {
				glog.Errorf("adding supported device identifier '%d' to device provider failed: %s", v, err.Error())
			}
		}
	}
	return nil
}

func (rm *ResourceManager) GetAllocateCh() chan types.AllocationInfo {
	return rm.rFactory.GetAllocateCh()
}

func (rm *ResourceManager) GetMountedCh() chan []string {
	return rm.rFactory.GetMountedCh()
}

func (rm *ResourceManager) GetDeallocateCh() chan types.DeallocationInfo {
	return rm.rFactory.GetDeallocateCh()
}
