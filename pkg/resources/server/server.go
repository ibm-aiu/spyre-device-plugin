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

package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/hashicorp/go-multierror"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice"
	config "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/dma"
	spyrert "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/runtime"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

type resourceServer struct {
	resourcePool       types.ResourcePool
	pluginWatch        bool
	endPoint           string // Socket file
	sockPath           string // Socket file path
	resourceNamePrefix string
	grpcServer         *grpc.Server
	termSignal         chan bool
	updateSignal       chan bool
	stopWatcher        chan bool
	checkIntervals     int // health check intervals in seconds
	spyreClient        *spyreclient.SpyreClient
	allocatedCh        chan types.AllocationInfo
	topology           *pcitopov2.Pcitopo
	serverStarted      atomic.Bool
	processing         atomic.Bool
}

const (
	rsWatchInterval    = 5 * time.Second
	serverStartTimeout = 5 * time.Second
	pcitopoPathDefault = "/usr/bin/pcitopo"
)

var (
	DeviceAllocationTrialLimit = 10
)

func (rs *resourceServer) GetResourcePool() types.ResourcePool {
	return rs.resourcePool
}

// NewResourceServer returns an instance of ResourceServer
func NewResourceServer(prefix, suffix string, pluginWatch bool, rp types.ResourcePool, spyreClient *spyreclient.SpyreClient,
	allocatedCh chan types.AllocationInfo, topologyFilepath string) types.ResourceServer {

	sockName := fmt.Sprintf("%s_%s.%s", prefix, rp.GetResourceName(), suffix)
	sockPath := filepath.Join(types.SockDir, sockName)
	var topology *pcitopov2.Pcitopo
	topo, err := spyretopo.GetPciTopology(topologyFilepath, false)
	if err == nil {
		topology = &topo
	} else {
		glog.Warningf("failed to get pci topology: %v", err)
	}

	if !pluginWatch {
		sockPath = filepath.Join(types.DeprecatedSockDir, sockName)
	}
	rs := &resourceServer{
		resourcePool:       rp,
		pluginWatch:        pluginWatch,
		endPoint:           sockName,
		sockPath:           sockPath,
		resourceNamePrefix: prefix,
		grpcServer:         grpc.NewServer(),
		termSignal:         make(chan bool, 1),
		updateSignal:       make(chan bool),
		stopWatcher:        make(chan bool),
		checkIntervals:     20, // updates every 20 seconds
		spyreClient:        spyreClient,
		allocatedCh:        allocatedCh,
		topology:           topology,
	}

	return rs
}

func (rs *resourceServer) register() error {
	kubeletEndpoint := "unix:" + filepath.Join(types.DeprecatedSockDir, types.KubeEndPoint)
	conn, err := grpc.NewClient(kubeletEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		glog.Errorf("%s device plugin unable connect to Kubelet : %v", rs.resourcePool.GetResourceName(), err)
		return err
	}
	defer conn.Close() //nolint:errcheck
	client := pluginapi.NewRegistrationClient(conn)

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     rs.endPoint,
		ResourceName: fmt.Sprintf("%s/%s", rs.resourceNamePrefix, rs.resourcePool.GetResourceName()),
	}

	if _, err = client.Register(context.Background(), request); err != nil {
		glog.Errorf("%s device plugin unable to register with Kubelet : %v", rs.resourcePool.GetResourceName(), err)
		return err
	}
	glog.Infof("%s device plugin registered with Kubelet", rs.resourcePool.GetResourceName())
	return nil
}

func (rs *resourceServer) GetInfo(ctx context.Context, rqt *registerapi.InfoRequest) (*registerapi.PluginInfo, error) {
	pluginInfoResponse := &registerapi.PluginInfo{
		Type:              registerapi.DevicePlugin,
		Name:              fmt.Sprintf("%s/%s", rs.resourceNamePrefix, rs.resourcePool.GetResourceName()),
		Endpoint:          filepath.Join(types.SockDir, rs.endPoint),
		SupportedVersions: []string{"v1alpha1", "v1beta1"},
	}
	return pluginInfoResponse, nil
}

func (rs *resourceServer) NotifyRegistrationStatus(ctx context.Context,
	regstat *registerapi.RegistrationStatus) (*registerapi.RegistrationStatusResponse, error) {
	if regstat.PluginRegistered {
		glog.V(1).Infof("Plugin: %s gets registered successfully at Kubelet\n", rs.endPoint)
	} else {
		glog.V(1).Infof("Plugin: %s failed to be registered at Kubelet: %v; restarting.\n", rs.endPoint, regstat.Error)
		rs.grpcServer.Stop()
	}
	return &registerapi.RegistrationStatusResponse{}, nil
}

func (rs *resourceServer) GetPreferredAllocation(ctx context.Context, rqt *pluginapi.PreferredAllocationRequest) (fResp *pluginapi.PreferredAllocationResponse, fErr error) { //nolint:lll
	glog.V(1).Infof("GetPreferredAllocationAllocation() called with %+v", rqt)
	resp := new(pluginapi.PreferredAllocationResponse)
	var err error
	for cIndex, cReq := range rqt.ContainerRequests {
		cResp := new(pluginapi.ContainerPreferredAllocationResponse)
		var allocatedDeviceIdList []string
		for j := 0; j < DeviceAllocationTrialLimit; j++ {
			allocatedDeviceIdList, err = spyredevice.AllocateDevices(ctx, rs.spyreClient, rs, cReq.AvailableDeviceIDs,
				cReq.AllocationSize)
			if err != nil {
				if spyredevice.IsConflictError(err) {
					glog.Errorf("resource update conflict occurred; try again.")
					continue
				} else {
					glog.Errorf("failed to allocate device: %v", err)
					return resp, err
				}
			}
			if allocatedDeviceIdList != nil {
				glog.Infof("allocation successfully finished for container %d: %s", cIndex, allocatedDeviceIdList)
				cResp.DeviceIDs = allocatedDeviceIdList
				break
			} else {
				return nil, fmt.Errorf("no allocatable device remains")
			}
		}
		resp.ContainerResponses = append(resp.ContainerResponses, cResp)
	}
	if err != nil {
		err = &errors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonNotFound,
			},
		}
	}
	glog.V(1).Infof("PreferredAllocationResponse send: %+v (%v)", resp, err)
	return resp, err
}

func (rs *resourceServer) Allocate(ctx context.Context, rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) { //nolint:lll
	glog.V(1).Infof("Allocate() called with %+v", rqt)
	resp := new(pluginapi.AllocateResponse)
	for _, container := range rqt.ContainerRequests {
		if err := rs.valid(container.DevicesIDs); err != nil {
			glog.Errorf("failed to validate allocation: %v", err)
			return resp, err
		}
		containerResp := new(pluginapi.ContainerAllocateResponse)
		containerResp.Devices = rs.resourcePool.GetDeviceSpecs(container.DevicesIDs)
		containerResp.Mounts = rs.resourcePool.GetMounts(container.DevicesIDs)
		containerResp.Envs = rs.getEnvs(container.DevicesIDs)
		resp.ContainerResponses = append(resp.ContainerResponses, containerResp)
		mntPoints := []string{}
		// inform SpyreDeviceSharedInfo
		for _, mnt := range containerResp.Mounts {
			if mnt.ContainerPath == config.GetConfigContainerPath() {
				mntPoints = append(mntPoints, mnt.HostPath)
			}
		}
		if !utils.IsReservationMode() {
			rs.allocatedCh <- types.AllocationInfo{
				DeviceIDs:    container.DevicesIDs,
				MountPoints:  mntPoints,
				ResourceName: rs.resourcePool.GetResourceName(),
			}
		}
		if dma.NeedP2PDMAConfigure(container.DevicesIDs) {
			if err := dma.SetDevResourceFilePermissions(container.DevicesIDs); err != nil {
				glog.V(2).Infof("failed to config for P2P DMA: %v, skip", err)
			}
		}
	}
	// relay for the next allocation
	glog.V(1).Infof("Wait for internal allocation process: %+v", resp)
	rs.WaitForNoAllocationInProcess()
	glog.V(1).Infof("AllocateResponse send: %+v", resp)
	return resp, nil
}

func (rs *resourceServer) ListAndWatch(empty *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	methodID := fmt.Sprintf("ListAndWatch(%s)", rs.resourcePool.GetResourceName()) // for logging purpose
	glog.V(1).Infof("%s invoked", methodID)

	// Send initial list of devices
	devs := make([]*pluginapi.Device, 0)
	resp := new(pluginapi.ListAndWatchResponse)
	rp := rs.resourcePool
	deviceMap := rp.GetDevices()
	if !utils.IsReservationMode() {
		if rp.IsTopologyAware() {
			deviceMap = spyretopo.GetMaxValidPeers(deviceMap, rp.GetResourceName(), rp.GetSelfAllocation())
		}
	}

	for _, dev := range deviceMap {
		devs = append(devs, dev)
	}
	resp.Devices = devs
	glog.V(1).Infof("%s: send devices %v\n", methodID, resp)

	if err := stream.Send(resp); err != nil {
		glog.Errorf("%s: error: cannot update device states: %v\n", methodID, err)
		rs.grpcServer.Stop()
		return err
	}

	glog.V(1).Infof("%s server started\n", methodID)
	rs.serverStarted.Store(true)

	// listen for events: if updateSignal send new list of devices
	for {
		select {
		case <-rs.termSignal:
			// Terminate signal received; return from method call
			glog.V(1).Infof("%s: terminate signal received", methodID)
			return nil
		case <-rs.updateSignal:
			// Device health changed; so send new device list
			glog.V(1).Infof("%s: device health changed!\n", methodID)
			newDevs := make([]*pluginapi.Device, 0)
			deviceMap := rp.GetDevices()
			if !utils.IsReservationMode() {
				if rp.IsTopologyAware() {
					selfAllocation := rp.GetSelfAllocation()
					deviceMap = spyretopo.GetMaxValidPeers(deviceMap, rp.GetResourceName(), selfAllocation)
					glog.V(1).Infof("%s: self-allocation %v -> new device map: %v\n", methodID, selfAllocation, deviceMap)
				}
			}
			for _, dev := range deviceMap {
				newDevs = append(newDevs, dev)
			}
			resp.Devices = newDevs

			glog.Infof("%s: send updated devices %v", methodID, resp)
			if err := stream.Send(resp); err != nil {
				glog.Errorf("%s: error: cannot update device states: %v\n", methodID, err)
				return err
			}
		}
	}
}

func (rs *resourceServer) PreStartContainer(ctx context.Context,
	psRqt *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (rs *resourceServer) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) { //nolint:lll
	glog.V(1).Infof("plugin option perDeviceAllocation: %s\n", os.Getenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey()))
	glog.V(1).Infof("plugin option topologyAwareAllocation: %s\n", os.Getenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey()))
	preferredAlloc := os.Getenv(spyrev1alpha1.PerDeviceAllocationMode.EnvKey()) == spyreconst.ModeEnabledValue ||
		os.Getenv(spyrev1alpha1.TopologyAwareAllocationMode.EnvKey()) == spyreconst.ModeEnabledValue
	glog.V(1).Infof("preferredAlloc: %v\n", preferredAlloc)
	return &pluginapi.DevicePluginOptions{
		PreStartRequired:                false,
		GetPreferredAllocationAvailable: preferredAlloc,
	}, nil
}

func (rs *resourceServer) Init() error {
	return nil
}

// gRPC server related
func (rs *resourceServer) Start() error {
	resourceName := rs.resourcePool.GetResourceName()
	_ = rs.cleanUp() // try tp clean up and continue

	glog.V(1).Infof("starting %s device plugin endpoint at: %s\n", resourceName, rs.endPoint)
	lis, err := net.Listen("unix", rs.sockPath)
	if err != nil {
		glog.Errorf("error starting %s device plugin endpoint: %v", resourceName, err)
		return err
	}

	// Register all services
	if rs.pluginWatch {
		registerapi.RegisterRegistrationServer(rs.grpcServer, rs)
	}
	pluginapi.RegisterDevicePluginServer(rs.grpcServer, rs)

	go func() {
		err := rs.grpcServer.Serve(lis)
		if err != nil {
			glog.Errorf("serving incoming requests failed: %s", err.Error())
		}
	}()
	conn, err := grpc.NewClient("unix:"+rs.sockPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		glog.Errorf("error. unable to establish test connection with %s gRPC server: %v", resourceName, err)
		return err
	}
	defer conn.Close() //nolint:errcheck

	glog.V(1).Infof("%s device plugin endpoint started serving", resourceName)

	if !rs.pluginWatch {
		// Register with Kubelet.
		err = rs.register()
		if err != nil {
			// Stop server
			rs.grpcServer.Stop()
			glog.Fatal(err)
			return err
		}
	}

	return nil
}

func (rs *resourceServer) restart() error {
	resourceName := rs.resourcePool.GetResourceName()
	glog.Infof("restarting %s device plugin server...", resourceName)
	if rs.grpcServer == nil {
		return fmt.Errorf("grpc server instance not found for %s", resourceName)
	}
	rs.grpcServer.Stop()
	rs.grpcServer = nil
	// Send terminate signal to ListAndWatch()
	rs.termSignal <- true

	rs.grpcServer = grpc.NewServer() // new instance of a grpc server
	return rs.Start()
}

func (rs *resourceServer) Stop() error {
	resourceName := rs.resourcePool.GetResourceName()
	glog.Infof("stopping %s device plugin server...", resourceName)
	if rs.grpcServer == nil {
		return nil
	}
	// Send terminate signal to ListAndWatch()
	rs.termSignal <- true
	if !rs.pluginWatch {
		rs.stopWatcher <- true
	}

	rs.grpcServer.Stop()
	rs.grpcServer = nil

	return rs.cleanUp()
}

func (rs *resourceServer) Watch() {
	// Watch for Kubelet socket file; if not present restart server
	for {
		select {
		case stop := <-rs.stopWatcher:
			if stop {
				glog.Infof("kubelet watcher stopped for server %s", rs.resourcePool.GetResourceName())
				return
			}
		default:
			_, err := os.Lstat(rs.sockPath)
			if err != nil {
				// Socket file not found; restart server
				glog.Warningf("server endpoint not found %s", rs.endPoint)
				glog.Warningf("most likely Kubelet restarted")
				if err := rs.restart(); err != nil {
					glog.Fatalf("unable to restart server %v", err)
				}
			}
		}
		// Sleep for some intervals
		time.Sleep(rsWatchInterval)
	}
}

func (rs *resourceServer) cleanUp() error {
	var result *multierror.Error

	if err := os.Remove(rs.sockPath); err != nil && !os.IsNotExist(err) {
		result = multierror.Append(result, err)
	}
	return result.ErrorOrNil()
}

func (rs *resourceServer) getEnvs(deviceIDs []string) map[string]string {
	envs := make(map[string]string)
	envVals := rs.resourcePool.GetEnvs(deviceIDs)
	values := ""
	lastIndex := len(envVals) - 1
	for i, s := range envVals {
		values += s
		if i == lastIndex {
			break
		}
		values += ","
	}
	envs[spyredevice.DeviceEnvKey] = values
	if config.IsMetricsEnabled() {
		envs[spyredevice.MetricPathEnvKey] = config.GetEnabledMetricPath()
	}
	return envs
}

func (rs *resourceServer) InformedBySharedInfo(deviceList []string, allocated bool, self bool) {
	rs.processing.Store(true)
	rp := rs.resourcePool
	rpImpl := spyredevice.GetResourcePoolImpl(rp)
	changed := rpImpl.InformedBySharedInfo(deviceList, allocated, self)
	if changed && rs.serverStarted.Load() {
		glog.Infof("%s calls update after informed %v (%v)", rs.resourcePool.GetResourceName(), deviceList, allocated)
		rs.updateSignal <- true
	}
	rs.processing.Store(false)
}

func (rs *resourceServer) TriggerUpdate() {
	if rs.serverStarted.Load() {
		rs.updateSignal <- true
	}
}

func (rs *resourceServer) GetPciTopology() *pcitopov2.Pcitopo {
	return rs.topology
}

// WaitForNoAllocationInProcess makes sure that there is no allocation in process
// in allocatedCh to prevent shared resource pool conflict
func (rs *resourceServer) WaitForNoAllocationInProcess() {
	condition := make(chan bool)
	defer close(condition)
	deadline := time.Now().Add(10 * time.Second)
	go func() {
		for {
			if len(rs.allocatedCh) == 0 && !rs.processing.Load() {
				condition <- true
				return
			}
			time.Sleep(1 * time.Second)
			if time.Now().After(deadline) {
				condition <- false
				return
			}
		}
	}()
	select {
	case <-condition:
		return
	case <-time.After(time.Until(deadline)):
		return
	}
}

func (rs *resourceServer) valid(deviceIDs []string) error {
	rn := rs.resourcePool.GetResourceName()
	if !utils.IsReservationMode() {
		if spyrert.CheckConflict(rn, deviceIDs) {
			return fmt.Errorf("devices has been already mounted by other resource pool: %v", deviceIDs)
		}
	}
	if rs.resourcePool.IsTopologyAware() {
		if rs.topology == nil {
			return fmt.Errorf("%s's topology is nil", rn)
		}
		if strings.Contains(rn, "vf") {
			glog.V(1).Info("skip validating topology on vf resource (not supported yet)")
			return nil
		}
		return rs.topology.ValidateTier(rs.resourcePool.GetResourceName(), deviceIDs)
	}
	return nil
}
