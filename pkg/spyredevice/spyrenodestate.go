/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package spyredevice

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"
	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	spyrert "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/runtime"
	spyretopo "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	NodeStateResName     = "spyrenodestates"
	ClusterPolicyResName = "spyreclusterpolicies"

	waitingTime = 5
)

var NodeNameEnvKey = utils.NodeNameEnvKey

// Apply device status in the given resource pool to SpyreNodeState resource.
// If NodeStateClient is not specified, the client will be automatically configured with InClusterConfig().
// In most cases caller does not need to specify the client; text code may explicitly specify the client so that
// it can perform out-of-cluster test.
// skipTopologyUpdate: if true, only updates SpyreInterfaces/SpyreSSAInterfaces without updating Pcitopo.
// This is useful during hotplug events where topology should only be updated by pci_watcher.
func WriteSpyreInterfacesToNodeState(
	ctx context.Context,
	cfg *rest.Config,
	pciDevices []types.PciDevice,
	spyreClient *spyreclient.SpyreClient,
	skipTopologyUpdate bool,
	updatedUnhealthyDevices []spyrev1alpha1.UnhealthyDevice,
) (*spyrev1alpha1.SpyreNodeState, error) {

	if spyreClient == nil {
		return nil, fmt.Errorf("spyreClient is not initialized, skip writing SpyreNodeState")
	}
	var err error
	nodeName := os.Getenv(NodeNameEnvKey)
	glog.V(1).Infof("Collecting PCI Devices...")
	glog.V(1).Infof("Devices: %v", pciDevices)
	glog.V(1).Infof("Updated UnhealthyDevices: %v", updatedUnhealthyDevices)

	glog.V(1).Infof("Search SpyreNodeState for this node: %v", nodeName)

	nodeState, err := GetNodeStateForThisNode(ctx, spyreClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get SpyreNodeState for this node: %s", nodeName)
	}

	glog.V(1).Infof("Applying up-to-date device status to SpyreNodeState...")
	spyreInterfaceMap, spyreSSAInterfaceMap := getSpyreInterfaceMaps(pciDevices)
	specChanged, statusChanged := updateSpyreInterfacesChanges(
		nodeState, spyreInterfaceMap, spyreSSAInterfaceMap, updatedUnhealthyDevices,
	)

	status := nodeState.Status
	// This is tentative behavior until the time we enable the feature to automatically call pcitopo command.
	// User can override this data by "oc edit spyrens XXX".
	// Note: GetPciTopology now reads from the dynamic topology file which is already filtered.
	// by pci_watcher based on device health.
	if !skipTopologyUpdate {
		if err := spyretopo.EnsureDynamicTopologyFiltered(); err != nil {
			glog.V(1).Infof("Failed to ensure that topology is filtered: %v", err)
		}
		topo, err := spyretopo.GetPciTopology("", false)
		if err == nil {
			specChanged = updateSpyreInterfacesWithTopo(topo, nodeState, specChanged)
		} else if nodeState.Spec.Pcitopo != "" {
			// clean up previous topology
			specChanged = true
			nodeState.Spec.Pcitopo = ""
		}
	}
	if specChanged {
		glog.V(1).Infof("update SpyreNodeState resource")
		if _, err = spyreClient.Update(ctx, nodeState, true); err != nil {
			glog.Errorf("failed to update SpyreNodeStateSpec: %v", err)
			return nil, err
		}
		glog.V(1).Info("Updated SpyreNodeStateSpec on WriteSpyreInterfacesToNodeState")
	}

	if statusChanged {
		// nodeState.Status can be overridden when patching the spec
		nodeState.Status.Conditions = status.Conditions
		nodeState.Status.UnhealthyDevices = status.UnhealthyDevices
		if _, err = spyreClient.UpdateStatus(ctx, nodeState, true); err != nil {
			glog.Errorf("failed to update SpyreNodeStateStatus:s %v", err)
			return nil, err
		}
		glog.V(1).Infof("Updated SpyreNodeStateStatus on WriteSpyreInterfacesToNodeState: %v, unhealthyDevices: %v",
			nodeState.Status.Conditions, nodeState.Status.UnhealthyDevices)
	}

	return nodeState, nil
}

// InitAllocationList update AllocationList if missing at initial state
// only call only at the first call where updatedUnhealthyDevices is nil
func InitAllocationList(ctx context.Context, cfg *rest.Config,
	spyreClient *spyreclient.SpyreClient) (*spyrev1alpha1.SpyreNodeState, error) {
	glog.V(1).Info("Try updating initial allocation list")
	nodeName := os.Getenv(NodeNameEnvKey)
	nodeState, err := GetNodeStateForThisNode(ctx, spyreClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get SpyreNodeState for this node: %s", nodeName)
	}

	if len(nodeState.Status.AllocationList) == 0 && spyreconf.IsSomeContainerMounted() {
		glog.V(1).Infof("update SpyreNodeState status")
		var clientset *kubernetes.Clientset
		if clientset, err = kubernetes.NewForConfig(cfg); err == nil {
			if allocationList, err := spyrert.GetAllocationList(clientset); err != nil {
				glog.Error("failed to get allocation info: ", err)
			} else if len(allocationList) > 0 {
				glog.V(1).Infof("update missing AllocationList")
				nodeState.Status.AllocationList = allocationList
				if _, err = spyreClient.UpdateStatus(ctx, nodeState, true); err != nil {
					glog.Errorf("failed to update SpyreNodeStateStatus:s %v", err)
					return nil, err
				}
			}
		}
	}
	return nodeState, nil
}

func getSpyreInterfaceMaps(pciDevices []types.PciDevice) (map[string]*spyrev1alpha1.SpyreInterface,
	map[string]*spyrev1alpha1.SpyreSSAInterface) {
	spyreInterfaceMap := make(map[string]*spyrev1alpha1.SpyreInterface)
	spyreSSAInterfaceMap := make(map[string]*spyrev1alpha1.SpyreSSAInterface)
	// apply new pciDevices
	for _, pciDevice := range pciDevices {
		pciAddress := pciDevice.GetPciAddr()
		switch {
		case pciDevice.IsSriovPF():
			health := spyrev1alpha1.SpyreHealthy
			if pciDevice.GetHealth() != pluginapi.Healthy {
				health = spyrev1alpha1.SpyreUnhealthy
			}
			if info, found := spyreInterfaceMap[pciAddress]; !found {
				spyreInterfaceMap[pciAddress] = &spyrev1alpha1.SpyreInterface{
					PciAddress: pciAddress,
					NumVfs:     0,
					Health:     health,
				}
			} else {
				info.Health = health
			}
			glog.V(1).Infof("Device %s is a SR-IOV PF (%s)", pciAddress, health)
		case pciDevice.IsIsolatedVF():
			if _, found := spyreSSAInterfaceMap[pciAddress]; !found {
				health := spyrev1alpha1.SpyreHealthy
				if pciDevice.GetHealth() != pluginapi.Healthy {
					health = spyrev1alpha1.SpyreUnhealthy
				}
				spyreSSAInterfaceMap[pciAddress] = &spyrev1alpha1.SpyreSSAInterface{
					PciAddress: pciAddress,
					Health:     health,
				}
				glog.V(1).Infof("Device %s is an Isolated VF (%s)", pciAddress, health)
			}
		default:
			glog.V(1).Infof("Device %s is a VF associated with PF %s", pciAddress, pciDevice.GetPfPciAddr())
			pfAddress := pciDevice.GetPfPciAddr()
			if existEntry, found := spyreInterfaceMap[pfAddress]; !found {
				spyreInterfaceMap[pfAddress] = &spyrev1alpha1.SpyreInterface{
					PciAddress: pfAddress,
					NumVfs:     1,
					Vfs:        []string{pciAddress},
				}
			} else {
				existEntry.Vfs = append(existEntry.Vfs, pciAddress)
				sort.Strings(existEntry.Vfs)
				spyreInterfaceMap[pfAddress].NumVfs = len(existEntry.Vfs)
			}
		}
	}
	return spyreInterfaceMap, spyreSSAInterfaceMap
}

func updateSpyreInterfacesChanges(nodeState *spyrev1alpha1.SpyreNodeState,
	spyreInterfaceMap map[string]*spyrev1alpha1.SpyreInterface,
	spyreSSAInterfaceMap map[string]*spyrev1alpha1.SpyreSSAInterface,
	updatedUnhealthyDevices []spyrev1alpha1.UnhealthyDevice) (bool, bool) {
	var specChanged bool
	// Update device info if new info != existing info
	for pciAddress, info := range spyreInterfaceMap {
		index := containsDevice(nodeState, pciAddress)
		if index >= 0 {
			if nodeState.Spec.SpyreInterfaces[index].NumVfs != info.NumVfs ||
				!slices.Equal(nodeState.Spec.SpyreInterfaces[index].Vfs, info.Vfs) ||
				nodeState.Spec.SpyreInterfaces[index].Health != info.Health {
				nodeState.Spec.SpyreInterfaces[index] = *info
				glog.V(1).Infof("Device %s has updated (vfs=%v, health=%s)", pciAddress, info.Vfs, info.Health)
				specChanged = true
				continue
			}
			glog.V(1).Infof("SpyreInterface %s has already been registered in the spec of SpyreNodeState. skip adding it.",
				pciAddress)
			continue
		}
		nodeState.Spec.SpyreInterfaces = append(nodeState.Spec.SpyreInterfaces, *info)
		specChanged = true
	}
	for pciAddress, info := range spyreSSAInterfaceMap {
		index := containsSSADevice(nodeState, pciAddress)
		if index >= 0 {
			if nodeState.Spec.SpyreSSAInterfaces[index].Health != info.Health {
				nodeState.Spec.SpyreSSAInterfaces[index] = *info
				glog.V(1).Infof("Device %s has updated (health=%s)", pciAddress, info.Health)
				specChanged = true
				continue
			}
			glog.V(1).Infof("SpyreSSAInterface %s has already been registered in the spec of SpyreNodeState. skip adding it.",
				pciAddress)
			continue
		}
		nodeState.Spec.SpyreSSAInterfaces = append(nodeState.Spec.SpyreSSAInterfaces, *info)
		specChanged = true
	}
	if updatedUnhealthyDevices == nil {
		updatedUnhealthyDevices = []spyrev1alpha1.UnhealthyDevice{}
	}
	// Update missing devices (previously exists but not reported by checker)
	for i, spyreInterface := range nodeState.Spec.SpyreInterfaces {
		pciAddr := spyreInterface.PciAddress
		if _, exists := spyreInterfaceMap[pciAddr]; !exists {
			if nodeState.Spec.SpyreInterfaces[i].Health != spyrev1alpha1.SpyreUnhealthy {
				glog.Infof("Device %s is missing, marking it as unhealthy", pciAddr)
				nodeState.Spec.SpyreInterfaces[i].Health = spyrev1alpha1.SpyreUnhealthy
				updatedUnhealthyDevices = append(updatedUnhealthyDevices, spyrev1alpha1.UnhealthyDevice{
					ID:    pciAddr,
					State: pb.DEVICE_STATE_REMOVED.String(),
				})
				specChanged = true
			}
		}
	}
	for i, ssaInterface := range nodeState.Spec.SpyreSSAInterfaces {
		pciAddr := ssaInterface.PciAddress
		if _, exists := spyreSSAInterfaceMap[pciAddr]; !exists {
			if nodeState.Spec.SpyreSSAInterfaces[i].Health != spyrev1alpha1.SpyreUnhealthy {
				glog.Infof("Device %s is missing, marking it as unhealthy", pciAddr)
				nodeState.Spec.SpyreSSAInterfaces[i].Health = spyrev1alpha1.SpyreUnhealthy
				updatedUnhealthyDevices = append(updatedUnhealthyDevices, spyrev1alpha1.UnhealthyDevice{
					ID:    pciAddr,
					State: pb.DEVICE_STATE_REMOVED.String(),
				})
				specChanged = true
			}
		}
	}
	statusChanged := mergePatchUpdatedUnhealthyDevice(nodeState, updatedUnhealthyDevices)
	return specChanged, statusChanged
}

// mergeUpdatedUnhealthyDevice merges the updatedUnhealthyDevices and existing unhealthy device list.
// return whether status is changed.
func mergePatchUpdatedUnhealthyDevice(nodeState *spyrev1alpha1.SpyreNodeState,
	updatedUnhealthyDevices []spyrev1alpha1.UnhealthyDevice) bool {
	unhealthDeviceMap := make(map[string]spyrev1alpha1.UnhealthyDevice, len(nodeState.Status.UnhealthyDevices))
	mergedUnhealthyDevices := []spyrev1alpha1.UnhealthyDevice{}
	var unhealthyListChange, conditionChanged bool
	// Init with existing list
	for _, dev := range nodeState.Status.UnhealthyDevices {
		unhealthDeviceMap[dev.ID] = dev
	}
	// Merge with updated list
	for _, dev := range updatedUnhealthyDevices {
		if previous, found := unhealthDeviceMap[dev.ID]; !found || (found && previous.State != dev.State) {
			unhealthyListChange = true
		}
		unhealthDeviceMap[dev.ID] = dev
	}
	for _, spyreInterface := range nodeState.Spec.SpyreInterfaces {
		if spyreInterface.Health == spyrev1alpha1.SpyreUnhealthy {
			if dev, found := unhealthDeviceMap[spyreInterface.PciAddress]; found {
				mergedUnhealthyDevices = append(mergedUnhealthyDevices, dev)
			} else {
				mergedUnhealthyDevices = append(mergedUnhealthyDevices, spyrev1alpha1.UnhealthyDevice{
					ID:    spyreInterface.PciAddress,
					State: pb.DEVICE_STATE_DEVICE_STATE_UNSPECIFIED.String(),
				})
			}
		}
	}
	for _, ssaInterface := range nodeState.Spec.SpyreSSAInterfaces {
		if ssaInterface.Health == spyrev1alpha1.SpyreUnhealthy {
			if dev, found := unhealthDeviceMap[ssaInterface.PciAddress]; found {
				mergedUnhealthyDevices = append(mergedUnhealthyDevices, dev)
			} else {
				mergedUnhealthyDevices = append(mergedUnhealthyDevices, spyrev1alpha1.UnhealthyDevice{
					ID:    ssaInterface.PciAddress,
					State: pb.DEVICE_STATE_DEVICE_STATE_UNSPECIFIED.String(),
				})
			}
		}
	}

	if len(mergedUnhealthyDevices) != len(nodeState.Status.UnhealthyDevices) {
		unhealthyListChange = true
	}

	// Update condition
	hasDevice := len(nodeState.Spec.SpyreSSAInterfaces)+len(nodeState.Spec.SpyreInterfaces) > 0
	spyreNodeStateHealthCondition := NewSpyreNodeStateHealthCondition(hasDevice, mergedUnhealthyDevices)
	if len(nodeState.Status.Conditions) == 0 {
		nodeState.Status.Conditions = []metav1.Condition{spyreNodeStateHealthCondition.FirstCondition()}
		conditionChanged = true
	} else {
		for i, condition := range nodeState.Status.Conditions {
			if condition.Type == ConditionTypeDeviceHealthy {
				conditionChanged, nodeState.Status.Conditions[i] = spyreNodeStateHealthCondition.UpdateCondition(condition.Status,
					condition.Message, condition.DeepCopy().LastTransitionTime)
				break
			}
		}
	}

	if unhealthyListChange {
		if len(mergedUnhealthyDevices) == 0 {
			nodeState.Status.UnhealthyDevices = nil // omit empty
		} else {
			nodeState.Status.UnhealthyDevices = mergedUnhealthyDevices
		}
	}
	return unhealthyListChange || conditionChanged
}

func updateSpyreInterfacesWithTopo(topo pcitopov2.Pcitopo, nodeState *spyrev1alpha1.SpyreNodeState, changed bool) bool {
	newTopo := topo.String()
	if nodeState.Spec.Pcitopo != newTopo {
		changed = true
		nodeState.Spec.Pcitopo = newTopo
	}
	if topo.Version == 0 {
		// cannot sync
		return changed
	}
	return changed
}

func containsDevice(nodeState *spyrev1alpha1.SpyreNodeState, pciAddress string) int {
	for index, si := range nodeState.Spec.SpyreInterfaces {
		if si.PciAddress == pciAddress {
			return index
		}
	}
	return -1
}

func containsSSADevice(nodeState *spyrev1alpha1.SpyreNodeState, pciAddress string) int {
	for index, si := range nodeState.Spec.SpyreSSAInterfaces {
		if si.PciAddress == pciAddress {
			return index
		}
	}
	return -1
}

// Returns a deep copy of SpyreNodeState resource of the node on which the device plugin runs.
func GetNodeStateForThisNode(
	ctx context.Context, spyreClient *spyreclient.SpyreClient) (*spyrev1alpha1.SpyreNodeState, error) {
	nodeName := os.Getenv(NodeNameEnvKey)
	return spyreClient.Get(ctx, nodeName)
}

func AllocateDevices(ctx context.Context, spyreClient *spyreclient.SpyreClient, rs types.ResourceServer,
	availableDeviceIDs []string, nDev int32) ([]string, error) {

	if utils.IsReservationMode() {
		return allocateReservedDevices(ctx, spyreClient, rs.GetResourcePool().GetResourceName(), availableDeviceIDs, nDev)
	}

	glog.V(1).Infof("Wait for internal allocation process before AllocateDevices")
	rs.WaitForNoAllocationInProcess()

	resourceName := rs.GetResourcePool().GetResourceName()
	if strings.HasPrefix(resourceName, "spyre_vf") {
		if hasIsolatedVFsAvailable(availableDeviceIDs) {
			selectedDeviceIdList, err := allocateIsolatedVfDevices(availableDeviceIDs, nDev)
			if err != nil {
				glog.Errorf("Isolated VF allocation failed: %v", err)
				return nil, err
			}
			return selectedDeviceIdList, nil
		}
	}

	deviceMap := rs.GetResourcePool().GetDevices()
	selectedDeviceIdList := allocateFromDeviceMap(availableDeviceIDs, nDev, deviceMap)

	// add delay for status update, in the case of resource release
	if selectedDeviceIdList == nil {
		time.Sleep(waitingTime * time.Second)
		glog.Infof("retry collecting info after %d seconds\n availableDeviceIDs: %v \n deviceMap: %v \n request: %d",
			waitingTime, availableDeviceIDs, deviceMap, nDev)
		deviceMap = rs.GetResourcePool().GetDevices()
		selectedDeviceIdList = allocateFromDeviceMap(availableDeviceIDs, nDev, deviceMap)
	}

	if selectedDeviceIdList == nil || len(selectedDeviceIdList) < int(nDev) {
		return selectedDeviceIdList, &errors.StatusError{
			ErrStatus: metav1.Status{
				Reason: metav1.StatusReasonConflict,
			},
		}
	}

	return selectedDeviceIdList, nil
}

func allocateReservedDevices(ctx context.Context, spyreClient *spyreclient.SpyreClient, resourceName string,
	availableDeviceIDs []string, nDev int32) ([]string, error) {

	// to avoid boundary case
	if nDev == 0 {
		return []string{}, nil
	}

	var err error

	glog.V(1).Infof("Retrieving SpyreNodeState for this node: %s", os.Getenv(NodeNameEnvKey))
	nodeState, err := GetNodeStateForThisNode(ctx, spyreClient)
	if err != nil {
		return nil, err
	}

	glog.V(1).Infof("Checking reservation")
	r, exists := nodeState.Status.Reservations[resourceName]
	if !exists {
		err = fmt.Errorf("unable to find reservation for resource: %s", resourceName)
		return nil, err
	}

	// select deviceSet
	var devSet []string
	for _, devSet = range r.DeviceSets {
		// with this condition, do not allow subset selection
		if len(devSet) == int(nDev) {
			unavailableFound := false
			// check availability of reserved devices
			for _, i := range devSet {
				if !slices.Contains(availableDeviceIDs, i) {
					unavailableFound = true
					break
				}
			}
			if !unavailableFound {
				break
			}
		}
	}

	if len(devSet) != int(nDev) {
		err = fmt.Errorf("unable to find device set for %d resources for: %s (reserved: %v)",
			int(nDev), resourceName, r.DeviceSets)
		return nil, err
	}

	return devSet, err
}

func allocateFromDeviceMap(availableDeviceIDs []string, nDev int32, deviceMap map[string]*pluginapi.Device) []string {
	selectedDeviceIdList := []string{}
	for _, d := range availableDeviceIDs {
		if _, found := deviceMap[d]; found {
			selectedDeviceIdList = append(selectedDeviceIdList, d)
			if len(selectedDeviceIdList) == int(nDev) { // cast to int because max(int) >= max(int32)
				break
			}
		}
	}
	if len(selectedDeviceIdList) < int(nDev) {
		glog.Infof("unable to find unallocated device: given device IDs: %v, device map: %v",
			availableDeviceIDs, deviceMap)
		glog.Infof("not an error, but return with empty selected device ID")
		return nil
	}
	return selectedDeviceIdList
}

func IsConflictError(err error) bool {
	if statusError, ok := err.(*errors.StatusError); ok {
		return statusError.ErrStatus.Reason == metav1.StatusReasonConflict
	}
	return false
}

// hasIsolatedVFsAvailable checks if there are any isolated VFs available in the device list
func hasIsolatedVFsAvailable(availableDeviceIDs []string) bool {
	topo, err := spyretopo.GetPciTopology("/usr/local/etc/device-plugins/metadata/topo.json", false)
	if err != nil {
		glog.V(1).Infof("Failed to get PCI topology: %v", err)
		return false
	}

	if topo.SpyreVfDevices == nil {
		return false
	}

	for _, deviceID := range availableDeviceIDs {
		if _, exists := topo.SpyreVfDevices[deviceID]; exists {
			return true
		}
	}
	return false
}

// allocateIsolatedVfDevices handles isolated VF allocation (no fallback to regular VFs)
func allocateIsolatedVfDevices(availableDeviceIDs []string, nDev int32) ([]string, error) {

	glog.V(1).Infof("Attempting isolated VF allocation for %d devices", nDev)

	topo, err := spyretopo.GetPciTopology("/usr/local/etc/device-plugins/metadata/topo.json", false)
	if err != nil {
		glog.Errorf("Failed to get PCI topology: %v", err)
		return nil, fmt.Errorf("failed to get PCI topology: %v", err)
	}

	if topo.SpyreVfDevices == nil {
		glog.V(1).Infof("No isolated VFs available in topology")
		return nil, fmt.Errorf("no isolated VFs available")
	}

	isolatedVfDevices := make([]string, 0)
	for _, deviceID := range availableDeviceIDs {
		if _, exists := topo.SpyreVfDevices[deviceID]; exists {
			glog.V(1).Infof("Device %s is an isolated VF", deviceID)
			isolatedVfDevices = append(isolatedVfDevices, deviceID)
		}
	}

	if len(isolatedVfDevices) < int(nDev) {
		glog.V(1).Infof("Not enough isolated VFs available: requested %d, available %d",
			nDev, len(isolatedVfDevices))
		return nil, fmt.Errorf("not enough isolated VFs available")
	}

	selectedDevices := make([]string, 0, int(nDev))
	for i := 0; i < int(nDev) && i < len(isolatedVfDevices); i++ {
		selectedDevices = append(selectedDevices, isolatedVfDevices[i])
	}

	glog.V(1).Infof("Successfully allocated isolated VFs: %v", selectedDevices)
	return selectedDevices, nil
}
