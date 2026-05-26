/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	"github.com/ibm-aiu/spyre-operator/controllers/spyrepod"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

var (
	RuntimeUnixSocketKey     = "RUNTIME_UNIX_SOCK"
	defaultCrioSocketPath    = "unix:///var/run/crio/crio.sock"
	KubeletUnixSocketKey     = "KUBELET_UNIX_SOCK"
	defaultKubeletSocketPath = "unix:/var/lib/kubelet/pod-resources/kubelet.sock"
)

type HostMountInfo struct {
	ConfigHostPath string
	MetricsHostPth string
}

func GetRuntimeUnixSocketPath() string {
	return utils.GetEnvOrDefault(RuntimeUnixSocketKey, defaultCrioSocketPath)
}

func GetKubeletUnixSocketPath() string {
	return utils.GetEnvOrDefault(KubeletUnixSocketKey, defaultKubeletSocketPath)
}

type AllocationInfo struct {
	Allocation     map[string]bool
	AllocationList []spyrev1alpha1.Allocation
}

func NewAllocationInfo() *AllocationInfo {
	return &AllocationInfo{Allocation: make(map[string]bool), AllocationList: []spyrev1alpha1.Allocation{}}
}

func (info *AllocationInfo) Allocated(deviceID string) bool {
	if allocated, found := info.Allocation[deviceID]; found {
		return allocated
	}
	return false
}

// listMountPoints returns mapping from host path to container id from the container runtime LIST API call
func listMountPoints() (map[string]string, error) {
	ctx := context.Background()
	// Dial CRI-O's gRPC API over Unix socket
	conn, err := grpc.NewClient(GetRuntimeUnixSocketPath(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to CRI-O: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	// Create a new CRI-O client
	client := v1.NewRuntimeServiceClient(conn)

	// Define the request to list all containers
	listContainersRequest := &v1.ListContainersRequest{}

	// Retrieve list of containers
	listContainersResponse, err := client.ListContainers(context.Background(), listContainersRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %v", err)
	}

	mountPoints := make(map[string]string)
	for _, container := range listContainersResponse.GetContainers() {
		if container.State == v1.ContainerState_CONTAINER_EXITED || container.State == v1.ContainerState_CONTAINER_UNKNOWN {
			continue
		}
		hostMntInfo, _ := getContainerMountHostPath(ctx, client, container.Id)
		if hostMntInfo.ConfigHostPath != "" {
			mountPoints[hostMntInfo.ConfigHostPath] = container.Id
		}
		if hostMntInfo.MetricsHostPth != "" {
			mountPoints[hostMntInfo.MetricsHostPth] = container.Id
		}
	}
	return mountPoints, nil
}

func GetMountHostPath(containerId string) (HostMountInfo, error) {
	// we should probably parametrize the context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Dial CRI-O's gRPC API over Unix socket
	conn, err := grpc.NewClient(GetRuntimeUnixSocketPath(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		glog.Errorf("failed to connect to CRI-O: %v", err)
		return HostMountInfo{}, err
	}
	defer conn.Close() //nolint:errcheck

	// Create a new CRI-O client
	client := v1.NewRuntimeServiceClient(conn)
	mntHostPaths, err := getContainerMountHostPath(ctx, client, containerId)
	return mntHostPaths, err
}

func getContainerMountHostPath(ctx context.Context, client v1.RuntimeServiceClient,
	containerId string) (HostMountInfo, error) {

	containerRequest := &v1.ContainerStatusRequest{
		ContainerId: containerId,
	}
	statusResponse, err := client.ContainerStatus(ctx, containerRequest)
	hostMntInfo := HostMountInfo{}
	if err == nil {
		mnts := statusResponse.Status.GetMounts()
		glog.V(1).Infof("mounts: %v", mnts)
		for _, mnt := range mnts {
			switch {
			case spyreconf.IsConfigMnt(mnt.ContainerPath, mnt.HostPath):
				hostMntInfo.ConfigHostPath = mnt.HostPath
			case spyreconf.IsMetricsMnt(mnt.ContainerPath, mnt.HostPath):
				hostMntInfo.MetricsHostPth = mnt.HostPath
			}
		}
	}
	return hostMntInfo, err
}

// CleanUnmountedHostPath removes unmounted config path
// This function check mount points of all running containers
func CleanUnmountedHostPath() (map[string][]string, error) {
	unmountedDevices := make(map[string][]string)
	configHostMounts, err := spyreconf.ListAllMounts(spyreconf.GetConfigHostPath())
	if err != nil {
		return unmountedDevices, err
	}
	metriscHostMounts, err := spyreconf.ListAllMounts(spyreconf.GetMetricsHostPath())
	if err != nil {
		glog.Warningf("failed to list metrics host path from: %s, reason: %s", spyreconf.GetMetricsHostPath(), err.Error())
	}
	mountPoints, err := listMountPoints()
	glog.Infof("mount points: %v", mountPoints)
	if err != nil {
		return unmountedDevices, err
	}
	for _, mnt := range configHostMounts {
		if _, found := mountPoints[mnt]; !found {
			if deviceIDs, readErr := spyreconf.ReadSenlibConfig(mnt); readErr != nil {
				glog.Warningf("failed to read senlib config %s: %v", mnt, readErr)
			} else {
				resourceName, _ := spyreconf.ReadResourcePool(mnt)
				unmountedDevices[resourceName] = append(unmountedDevices[resourceName], deviceIDs...)
			}
			if err := os.RemoveAll(mnt); err != nil {
				glog.Warningf("failed to clean %s", mnt)
			} else {
				glog.Infof("clean unmounted %s", mnt)
			}
		}
	}
	for _, mnt := range metriscHostMounts {
		if err := os.RemoveAll(mnt); err != nil {
			glog.Warningf("failed to clean %s", mnt)
		} else {
			glog.Infof("clean unmounted %s", mnt)
		}
	}
	return unmountedDevices, nil
}

func GetAllocationList(clientset *kubernetes.Clientset) ([]spyrev1alpha1.Allocation, error) {
	fieldSelector := fmt.Sprintf("spec.nodeName=%s", utils.GetNodeName())
	listOptions := metav1.ListOptions{
		FieldSelector: fieldSelector,
	}
	allocationList := []spyrev1alpha1.Allocation{}
	initialPodList, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), listOptions)
	if err != nil {
		return allocationList, err
	}
	for _, p := range initialPodList.Items {
		if spyrepod.IsSpyrePod(&p) {
			allocatedDeviceIDs, _, _ := GetDevicesAndMounts(&p)
			resourceName := utils.GetResourceNameFromPod(&p)
			if len(allocatedDeviceIDs) > 0 {
				allocation := spyrev1alpha1.Allocation{
					Pod: &spyrev1alpha1.Pod{
						Name:      p.Name,
						Namespace: p.Namespace,
					},
					DeviceList:   allocatedDeviceIDs,
					ResourcePool: resourceName,
				}
				allocationList = append(allocationList, allocation)
			}
		}
	}
	return allocationList, nil
}

func GetDevicesAndMounts(p *corev1.Pod) ([]string, []string, error) {
	allDeviceIDs := []string{}
	mntHostPaths := []string{}
	containerStatuses := p.Status.ContainerStatuses
	var err error
	for _, status := range containerStatuses {
		splits := strings.Split(status.ContainerID, "/")
		containerId := splits[len(splits)-1]
		if containerId != "" {
			var containerMntHostInfo HostMountInfo
			containerMntHostInfo, err = GetMountHostPath(containerId)
			if err != nil {
				glog.Errorf("failed to get mount path of %s", containerId)
				break
			}
			configHostPath := containerMntHostInfo.ConfigHostPath
			if configHostPath == "" {
				continue
			}
			if _, err = os.Stat(configHostPath); errors.Is(err, os.ErrNotExist) {
				continue // path removed
			}
			if deviceIDs, readErr := spyreconf.ReadSenlibConfig(configHostPath); readErr != nil {
				glog.Errorf("failed to read senlib config %s: %v", configHostPath, readErr)
			} else {
				allDeviceIDs = append(allDeviceIDs, deviceIDs...)
			}
			mntHostPaths = append(mntHostPaths, containerMntHostInfo.ConfigHostPath)
			mntHostPaths = append(mntHostPaths, containerMntHostInfo.MetricsHostPth)
		}
		return allDeviceIDs, mntHostPaths, err
	}
	return allDeviceIDs, mntHostPaths, err
}

// CheckConflict returns true if any of device in the list has been mounted to the other container
// (checked by runtime API)
func CheckConflict(resourcePool string, deviceIDs []string) bool {
	copiedDeviceIDs := append([]string(nil), deviceIDs...)
	mountPoints, err := listMountPoints()
	if err != nil {
		glog.Errorf("CheckConflict: failed to list mount point: %v", err)
		return false
	}
	mountedDevices := []string{}
	for mnt := range mountPoints {
		if !strings.Contains(mnt, config.SpyreConfigBaseFolderName) {
			continue // irrelevant mount point
		}
		if _, err = os.Stat(mnt); errors.Is(err, os.ErrNotExist) {
			continue // path removed
		}
		if deviceIDs, readErr := spyreconf.ReadSenlibConfig(mnt); readErr != nil {
			glog.Errorf("CheckConflict: failed to read senlib config %s: %v", mnt, readErr)
		} else {
			if mountedRp, err := spyreconf.ReadResourcePool(mnt); err == nil && resourcePool != mountedRp {
				mountedDevices = append(mountedDevices, deviceIDs...)
			}
		}
	}
	return checkConflict(copiedDeviceIDs, mountedDevices)
}

func checkConflict(a, b []string) bool {
	sort.Strings(a)
	sort.Strings(b)
	for _, item := range a {
		for i := 0; i < len(b); i++ {
			if item < b[i] {
				break
			}
			if item == b[i] {
				return true
			}
		}
	}
	return false
}
