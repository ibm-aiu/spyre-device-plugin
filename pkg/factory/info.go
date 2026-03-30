/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

// info.go: keep device status in sync

package factory

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/golang/glog"
	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
)

const (
	// WaitTimeBeforeCleanUnclaim is the threshold time before cleanup allocated devices those are unclaimed.
	WaitTimeBeforeCleanUnclaim = 30 * time.Second
	ChannelBufferSize          = 1000
)

type SpyreDeviceSharedInfo struct {
	servers              []types.ResourceServer
	allocatedCh          chan types.AllocationInfo
	mountedCh            chan []string
	deallocatedCh        chan types.DeallocationInfo
	allocation           map[string]types.AllocationInfo
	mu                   sync.RWMutex
	stop                 chan bool
	reservedPathWithTime map[string]time.Time
	cleanupMu            sync.RWMutex
}

func NewSpyreDeviceSharedInfo() *SpyreDeviceSharedInfo {
	return &SpyreDeviceSharedInfo{
		servers:              []types.ResourceServer{},
		allocatedCh:          make(chan types.AllocationInfo, ChannelBufferSize),
		mountedCh:            make(chan []string, ChannelBufferSize),
		deallocatedCh:        make(chan types.DeallocationInfo, ChannelBufferSize),
		allocation:           make(map[string]types.AllocationInfo),
		stop:                 make(chan bool),
		reservedPathWithTime: make(map[string]time.Time),
	}
}

func (info *SpyreDeviceSharedInfo) register(server types.ResourceServer) {
	info.servers = append(info.servers, server)
}

// read allocate and deallocate channel until quit
func (info *SpyreDeviceSharedInfo) Run() {
	// running ticker to cleanuncliam path
	ticker := time.NewTicker(WaitTimeBeforeCleanUnclaim)
	defer ticker.Stop()
	go info.periodicClean(ticker)

	for {
		select {
		case <-info.stop:
			glog.Infof("SpyreDeviceSharedInfo stop running")
			close(info.allocatedCh)
			close(info.deallocatedCh)
			close(info.stop)
			return
		case allocatedInfo, ok := <-info.allocatedCh:
			if ok && len(allocatedInfo.DeviceIDs) > 0 {
				if len(allocatedInfo.MountPoints) > 0 && utils.MountPathExists(allocatedInfo.MountPoints) {
					sort.Strings(allocatedInfo.MountPoints)
					info.updateAllocation(allocatedInfo)
					info.cleanupMu.Lock()
					now := time.Now()
					for _, mntPoint := range allocatedInfo.MountPoints {
						info.reservedPathWithTime[mntPoint] = now
					}
					info.cleanupMu.Unlock()
				}
			}
		case deallocatedInfo, ok := <-info.deallocatedCh:
			if ok && len(deallocatedInfo.DeviceIDs) > 0 {
				info.deleteAllocation(deallocatedInfo)
			}
		case mntHostPaths, ok := <-info.mountedCh:
			if ok {
				// claim reserved host paths
				info.cleanupMu.Lock()
				for _, mntPoint := range mntHostPaths {
					_, ok := info.reservedPathWithTime[mntPoint]
					if !ok {
						glog.Warningf("%s is not in the reserved path. It seems to deallocate devices before container created or device plugin has been restarted after the creation.", mntPoint) //nolint:lll
					}
					delete(info.reservedPathWithTime, mntPoint)
				}
				info.cleanupMu.Unlock()
			}
		}
	}
}

// notify notifies all resource pool for the device allocated/deallocated
func (info *SpyreDeviceSharedInfo) notify(resourceName string, deviceList []string, allocated bool) {
	glog.Infof("notify %d servers for %s: %v (%v)", len(info.servers), resourceName, deviceList, allocated)
	for _, server := range info.servers {
		if server.GetResourcePool().GetResourceName() == resourceName {
			// Only topology-aware pool needs self-inform
			if utils.IsTopologyAwareResource(resourceName) {
				server.InformedBySharedInfo(deviceList, allocated, true)
			}
			continue
		}
		server.InformedBySharedInfo(deviceList, allocated, false)
	}
}

func (info *SpyreDeviceSharedInfo) updateAllocation(newInfo types.AllocationInfo) {
	conflictInfos := make(map[string][]string)
	duplicate := false
	for _, deviceID := range newInfo.DeviceIDs {
		if existInfo, found := info.allocation[deviceID]; found {
			if existInfo.ResourceName == newInfo.ResourceName {
				if reflect.DeepEqual(existInfo.MountPoints, newInfo.MountPoints) {
					duplicate = true
					break
				}
			} else {
				conflictInfos[existInfo.ResourceName] = append(conflictInfos[existInfo.ResourceName], deviceID)
			}
		}
	}
	for resourceName, deviceIDs := range conflictInfos {
		info.mu.Lock()
		glog.Infof("info: unexpectedly found conflict, deallocate existing %s %v", resourceName, deviceIDs)
		info.notify(resourceName, deviceIDs, false)
		info.mu.Unlock()
	}
	if !duplicate {
		info.mu.Lock()
		glog.Infof("info: new allocate %s %v", newInfo.ResourceName, newInfo.DeviceIDs)
		for _, deviceID := range newInfo.DeviceIDs {
			info.allocation[deviceID] = newInfo
		}
		info.notify(newInfo.ResourceName, newInfo.DeviceIDs, true)
		info.mu.Unlock()
	} else {
		glog.Infof("info: unexpectedly found duplication, skip %v", newInfo)
	}
}

func (info *SpyreDeviceSharedInfo) deleteAllocation(deallocatedInfo types.DeallocationInfo) {
	info.mu.Lock()
	glog.Infof("info: deallocate %s %v", deallocatedInfo.ResourceName, deallocatedInfo.DeviceIDs)
	for _, deviceID := range deallocatedInfo.DeviceIDs {
		delete(info.allocation, deviceID)
	}
	info.notify(deallocatedInfo.ResourceName, deallocatedInfo.DeviceIDs, false)
	info.mu.Unlock()
}

func (info *SpyreDeviceSharedInfo) periodicClean(ticker *time.Ticker) {
	for t := range ticker.C {
		newMap := make(map[string]time.Time)
		info.cleanupMu.Lock()
		for path, reservedTime := range info.reservedPathWithTime {
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				continue // path removed
			}
			duration := t.Sub(reservedTime)
			// Check if the duration is greater than 4 seconds
			if duration > WaitTimeBeforeCleanUnclaim {
				info.forceDeallocateIfNoClaim(path)
			} else {
				newMap[path] = reservedTime
			}
		}
		info.reservedPathWithTime = newMap
		info.cleanupMu.Unlock()
	}
}
func (info *SpyreDeviceSharedInfo) forceDeallocateIfNoClaim(mntHostPath string) {
	deviceIDs, readErr := spyreconf.ReadSenlibConfig(mntHostPath)
	if readErr != nil {
		glog.Errorf("failed to read senlib config %s: %v", mntHostPath, readErr)
		return
	}
	resourceName, _ := spyreconf.ReadResourcePool(mntHostPath)
	if !utils.IsReservationMode() {
		info.deallocatedCh <- types.DeallocationInfo{
			DeviceIDs:    deviceIDs,
			ResourceName: resourceName,
		}
	}
	glog.Infof("%v from %s deallocated after %ds as there is no container claims for", deviceIDs, resourceName, WaitTimeBeforeCleanUnclaim) //nolint:lll
}
