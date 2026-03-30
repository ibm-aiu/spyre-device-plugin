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

package utils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.com/google/uuid"

	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
)

var (
	SysBusPci = "/sys/bus/pci/devices"
	devDir    = "/dev"
)

const (
	totalVfFile          = "sriov_totalvfs"
	configuredVfFile     = "sriov_numvfs"
	eswitchModeSwitchdev = "switchdev"

	uuidGenerateMaxRetry = 10
	NodeNameEnvKey       = "NODE_NAME"
)

// DetectPluginWatchMode returns true if plugins registry directory exist
func DetectPluginWatchMode(sockDir string) bool {
	if _, err := os.Stat(sockDir); err != nil {
		return false
	}
	return true
}

// GetPfAddr returns SRIOV PF pci address if a device is VF given its pci address.
// If device it not VF then it will return empty string
func GetPfAddr(pciAddr string) (string, error) {
	pfSymLink := filepath.Join(SysBusPci, pciAddr, "physfn")
	pciinfo, err := os.Readlink(pfSymLink)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("error getting PF for PCI device %s %v", pciAddr, err)
	}
	return filepath.Base(pciinfo), nil
}

// PfExist check if [devicePath]/[pciAddr] exists
func PfExist(pciAddr string) bool {
	pfPath := filepath.Join(SysBusPci, pciAddr)
	if _, err := os.Stat(pfPath); err != nil {
		return false
	}
	return true
}

// IsSriovPF check if a pci device SRIOV capable given its pci address
func IsSriovPF(pciAddr string) bool {
	totalVfFilePath := filepath.Join(SysBusPci, pciAddr, totalVfFile)
	if _, err := os.Stat(totalVfFilePath); err != nil {
		return false
	}
	// sriov_totalvfs file exists -> sriov capable
	return true
}

// IsIsolatedVF check if a pci device has link to a PF
func IsIsolatedVF(pciAddr string) bool {
	totalVfFilePath := filepath.Join(SysBusPci, pciAddr, "physfn")
	if _, err := os.Stat(totalVfFilePath); err != nil {
		return false
	}
	return true
}

// GetVFconfigured returns number of VF configured for a PF
func GetVFconfigured(pf string) int {
	configuredVfPath := filepath.Join(SysBusPci, pf, configuredVfFile)
	vfs, err := os.ReadFile(configuredVfPath)
	if err != nil {
		return 0
	}
	configuredVFs := bytes.TrimSpace(vfs)
	numConfiguredVFs, err := strconv.Atoi(string(configuredVFs))
	if err != nil {
		return 0
	}
	return numConfiguredVFs
}

// GetVFList returns a List containing PCI addr for all VF discovered in a given PF
func GetVFList(pf string) (vfList []string, err error) {
	vfList = make([]string, 0)
	pfDir := filepath.Join(SysBusPci, pf)
	_, err = os.Lstat(pfDir)
	if err != nil {
		err = fmt.Errorf("error. Could not get PF directory information for device: %s, Err: %v", pf, err)
		return
	}

	vfDirs, err := filepath.Glob(filepath.Join(pfDir, "virtfn*"))

	if err != nil {
		err = fmt.Errorf("error reading VF directories %v", err)
		return
	}

	// Read all VF directory and get add VF PCI addr to the vfList
	for _, dir := range vfDirs {
		dirInfo, err := os.Lstat(dir)
		if err == nil && (dirInfo.Mode()&os.ModeSymlink != 0) {
			linkName, err := filepath.EvalSymlinks(dir)
			if err == nil {
				vfLink := filepath.Base(linkName)
				vfList = append(vfList, vfLink)
			}
		}
	}
	return
}

// GetSriovVFcapacity returns SRIOV VF capacity
func GetSriovVFcapacity(pf string) int {
	totalVfFilePath := filepath.Join(SysBusPci, pf, totalVfFile)
	vfs, err := os.ReadFile(totalVfFilePath)
	if err != nil {
		return 0
	}
	totalvfs := bytes.TrimSpace(vfs)
	numvfs, err := strconv.Atoi(string(totalvfs))
	if err != nil {
		return 0
	}
	return numvfs
}

// GetDevNode returns the numa node of a PCI device, -1 if none is specified or error.
func GetDevNode(pciAddr string) int {
	devNodePath := filepath.Join(SysBusPci, pciAddr, "numa_node")
	node, err := os.ReadFile(devNodePath)
	if err != nil {
		return -1
	}
	node = bytes.TrimSpace(node)
	numNode, err := strconv.Atoi(string(node))
	if err != nil {
		return -1
	}
	return numNode
}

// ValidPciAddr validates PciAddr given as string with host system
func ValidPciAddr(addr string) (string, error) {
	// Check system pci address

	// sysbus pci address regex
	var validLongID = regexp.MustCompile(`^0{4}:[0-9a-f]{2}:[0-9a-f]{2}.[0-7]$`)
	var validShortID = regexp.MustCompile(`^[0-9a-f]{2}:[0-9a-f]{2}.[0-7]$`)

	if validLongID.MatchString(addr) {
		return addr, deviceExist(addr)
	} else if validShortID.MatchString(addr) {
		addr = "0000:" + addr // make short form to sysfs represtation
		return addr, deviceExist(addr)
	}

	return "", fmt.Errorf("invalid pci address %s", addr)
}

func deviceExist(addr string) error {
	devPath := filepath.Join(SysBusPci, addr)
	_, err := os.Lstat(devPath)
	if err != nil {
		return fmt.Errorf("error: unable to read device directory %s", devPath)
	}
	return nil
}

// SriovConfigured returns true if sriov_numvfs reads > 0 else false
func SriovConfigured(addr string) bool {
	return GetVFconfigured(addr) > 0
}

// ValidResourceName returns true if it contains permitted characters otherwise false
func ValidResourceName(name string) bool {
	// name regex
	var validString = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	return validString.MatchString(name)
}

// GetVFIODeviceFile returns a vfio device files for vfio-pci bound PCI device's PCI address
func GetVFIODeviceFile(dev string) (devFileHost, devFileContainer string, err error) {
	// Get iommu group for this device
	devPath := filepath.Join(SysBusPci, dev)
	_, err = os.Lstat(devPath)
	if err != nil {
		err = fmt.Errorf("GetVFIODeviceFile(): Could not get directory information for device: %s, Err: %v", dev, err)
		return devFileHost, devFileContainer, err
	}

	iommuDir := filepath.Join(devPath, "iommu_group")
	if err != nil {
		err = fmt.Errorf("GetVFIODeviceFile(): error reading iommuDir %v", err)
		return devFileHost, devFileContainer, err
	}

	dirInfo, err := os.Lstat(iommuDir)
	if err != nil {
		err = fmt.Errorf("GetVFIODeviceFile(): unable to find iommu_group %v", err)
		return devFileHost, devFileContainer, err
	}

	if dirInfo.Mode()&os.ModeSymlink == 0 {
		err = fmt.Errorf("GetVFIODeviceFile(): invalid symlink to iommu_group %v", err)
		return devFileHost, devFileContainer, err
	}

	linkName, err := filepath.EvalSymlinks(iommuDir)
	if err != nil {
		err = fmt.Errorf("GetVFIODeviceFile(): error reading symlink to iommu_group %v", err)
		return devFileHost, devFileContainer, err
	}
	devFileContainer = filepath.Join(devDir, "vfio", filepath.Base(linkName))
	devFileHost = devFileContainer

	// Get a file path to the iommu group name
	namePath := filepath.Join(linkName, "name")
	// Read the iommu group name
	// The name file will not exist on baremetal
	vfioName, errName := os.ReadFile(namePath)
	if errName == nil {
		vName := strings.TrimSpace(string(vfioName))

		// if the iommu group name == vfio-noiommu then we are in a VM, adjust path to vfio device
		if vName == "vfio-noiommu" {
			linkName = filepath.Join(filepath.Dir(linkName), "noiommu-"+filepath.Base(linkName))
			devFileHost = filepath.Join(devDir, "vfio", filepath.Base(linkName))
		}
	}
	return devFileHost, devFileContainer, err
}

// GetDriverName returns current driver attached to a pci device from its pci address
func GetDriverName(pciAddr string) (string, error) {
	driverLink := filepath.Join(SysBusPci, pciAddr, "driver")
	driverInfo, err := os.Readlink(driverLink)
	if err != nil {
		return "", fmt.Errorf("error getting driver info for device %s %v", pciAddr, err)
	}
	return filepath.Base(driverInfo), nil
}

// CreateNewConfigFolder generates new folder with the unique ID, returns host mount path or error
func CreateNewConfigFolder(spyreConfigPath string) (string, error) {
	var err error
	for attempt := 1; attempt <= uuidGenerateMaxRetry; attempt++ {
		newUuid := uuid.NewString()
		newFolder := filepath.Join(spyreConfigPath, newUuid)
		err = CreateFolderIfNotExists(newFolder)
		if err == nil {
			return newFolder, err
		}
	}
	return "", fmt.Errorf("cannot create new config folder (max retry: %d): %v", uuidGenerateMaxRetry, err)
}

// CreateNewMetricsFolder creates a corresponding metrics folder to the config path
func CreateNewMetricsFolder(metricsHostPath, configHostPath string) (string, error) {
	uuidValue := GetUuidFromPath(configHostPath)
	newFolder := filepath.Join(metricsHostPath, uuidValue)
	err := CreateFolderIfNotExists(newFolder)
	return newFolder, err
}

func GetUuidFromPath(hostConfigPath string) string {
	folderName := filepath.Base(hostConfigPath)
	if folderName == "." {
		return ""
	}
	return folderName
}

func CreateFolderIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, os.ModeDir|0755)
	}
	return nil
}

func GetEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		glog.V(1).Infof("%s value is empty, set `%s`", key, defaultValue)
		return defaultValue
	}
	return value
}

func Contains(sl []string, s string) bool {
	for _, e := range sl {
		if e == s {
			return true
		}
	}
	return false
}

func GetNodeName() string {
	var nodeName string
	var err error
	var found bool
	nodeName, found = os.LookupEnv(NodeNameEnvKey)
	if !found {
		glog.Info("NODENAME_ENV is not set, use os.Hostname()")
		nodeName, err = os.Hostname()
		if err != nil {
			glog.Warning("failed to get host name")
		}
	}
	glog.Infof("nodeName=%s\n", nodeName)
	return nodeName
}

// RemoveIndex removes item at specific index if valid. Otherwise return original slice
func RemoveAllocationIndex(a []spyrev1alpha1.Allocation, index int) []spyrev1alpha1.Allocation {
	if index < len(a) {
		if index == 0 {
			if len(a) == 1 {
				return []spyrev1alpha1.Allocation{}
			} else {
				return a[1:]
			}
		}
		if index == len(a)-1 {
			return a[0:index]
		} else {
			return append(a[0:index], a[index+1:]...)
		}
	}
	return a
}

func MountPathExists(paths []string) bool {
	for _, path := range paths {
		if !PathExists(path) {
			return false
		}
	}
	return true
}

func PathExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

func IsTopologyAwareResource(resourceName string) bool {
	return strings.Contains(resourceName, "tier")
}

func CopyFile(src, dest string) error {
	if srcFile, err := os.Open(src); err != nil {
		return err
	} else {
		defer srcFile.Close() //nolint:errcheck
		if destFile, err := os.Create(dest); err != nil {
			return err
		} else {
			defer destFile.Close() //nolint:errcheck
			if _, err = io.Copy(destFile, srcFile); err != nil {
				return err
			}
		}
	}
	return nil
}

func IsReservationMode() bool {
	return os.Getenv(spyrev1alpha1.ReservationMode.EnvKey()) == spyreconst.ModeEnabledValue
}

func IsPseudoDeviceMode() bool {
	return os.Getenv(spyrev1alpha1.PseudoDeviceMode.EnvKey()) == spyreconst.ModeEnabledValue
}

func WriteFile(folder, filename, value string) error {
	path := filepath.Join(folder, filename)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck
	_, err = file.WriteString(value)
	return err
}

func ReadStringInFile(folder, filename string) (string, error) {
	path := filepath.Join(folder, filename)
	content, err := os.ReadFile(path)
	if err == nil {
		return string(content), nil
	}
	return "", err
}
