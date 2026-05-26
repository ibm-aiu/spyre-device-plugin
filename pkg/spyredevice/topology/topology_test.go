/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package topology_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/topology"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"github.com/ibm-aiu/spyre-operator/pkg/types/pcitopov2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	pcitopoPath            = "../../../test/data/pcitopo"
	topologyFolder         = "../../../test/data/topology"
	topoV1FilePath         = "../../../test/data/topo.json"
	topoV2FilePath         = "../../../test/data/topo_v2.json"
	devicePath             = "../../../test/data/device_path"
	pseudoTopologyFilePath = "../../../images/init-container-assets/pseudo-topology.json"
	node1Name              = "node1"
	node2Name              = "node2"
)

var zeroDistance = 2
var oneDistance = 4
var twoDistance = 8

var deviceDistances map[string]map[string]int = map[string]map[string]int{
	"00:01": {
		"00:02": 2,
		"00:03": 2,
		"00:04": 2,
		"00:05": 4,
		"00:06": 4,
		"00:07": 4,
		"00:08": 4,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:02": {
		"00:03": 2,
		"00:04": 2,
		"00:05": 4,
		"00:06": 4,
		"00:07": 4,
		"00:08": 4,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:03": {
		"00:04": 2,
		"00:05": 4,
		"00:06": 4,
		"00:07": 4,
		"00:08": 4,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:04": {
		"00:05": 4,
		"00:06": 4,
		"00:07": 4,
		"00:08": 4,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:05": {
		"00:06": 2,
		"00:07": 2,
		"00:08": 2,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:06": {
		"00:07": 2,
		"00:08": 2,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:07": {
		"00:08": 2,
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:08": {
		"00:09": 8,
		"00:10": 8,
		"00:11": 8,
		"00:12": 8,
		"00:13": 8,
		"00:14": 8,
		"00:15": 8,
		"00:16": 8,
	},
	"00:09": {
		"00:10": 2,
		"00:11": 2,
		"00:12": 2,
		"00:13": 4,
		"00:14": 4,
		"00:15": 4,
		"00:16": 4,
	},
	"00:10": {
		"00:11": 2,
		"00:12": 2,
		"00:13": 4,
		"00:14": 4,
		"00:15": 4,
		"00:16": 4,
	},
	"00:11": {
		"00:12": 2,
		"00:13": 4,
		"00:14": 4,
		"00:15": 4,
		"00:16": 4,
	},
	"00:12": {
		"00:13": 4,
		"00:14": 4,
		"00:15": 4,
		"00:16": 4,
	},
	"00:13": {
		"00:14": 2,
		"00:15": 2,
		"00:16": 2,
	},
	"00:14": {
		"00:15": 2,
		"00:16": 2,
	},
	"00:15": {
		"00:16": 2,
	},
	"00:16": {},
}

var allDevices = getAllDevices()

var firstTier0Group = []string{"00:01", "00:02", "00:03", "00:04"}
var secondTier0Group = []string{"00:05", "00:06", "00:07", "00:08"}
var thirdTier0Group = []string{"00:09", "00:10", "00:11", "00:12"}
var fourthTier0Group = []string{"00:13", "00:14", "00:15", "00:16"}
var firstTier1Group = append(firstTier0Group, secondTier0Group...)
var secondTier1Group = append(thirdTier0Group, fourthTier0Group...)

func getAllDevices() []string {
	devices := make([]string, 0, len(deviceDistances))
	for dev := range deviceDistances {
		devices = append(devices, dev)
	}
	return devices
}

var noSelfAllocated = map[string]bool{}
var firstTier0SelfAllocated = map[string]bool{
	"00:01": true,
	"00:05": false, // released item
}
var moreThanOneTier0SelfAllocated = map[string]bool{
	"00:01": true,
	"00:05": true,
	"00:09": false, // released item
}
var allTier0SelfAllocated = map[string]bool{
	"00:01": true,
	"00:05": true,
	"00:09": true,
	"00:13": true,
}
var firstTier1SelfAllocated = map[string]bool{
	"00:01": true,
	"00:09": false, // released item
}
var allTier1SelfAllocated = map[string]bool{
	"00:01": true,
	"00:09": true,
}
var allAllocated = getAllAllocated()

func getAllAllocated() map[string]bool {
	allocation := make(map[string]bool)
	for dev := range deviceDistances {
		allocation[dev] = true
	}
	return allocation
}

func pfTierResourceName(suffix string) string {
	return spyreconst.PfResourceName + suffix
}

var tier0RemainCandidates = [][]string{
	{"00:02", "00:03", "00:04"},
	{"00:06", "00:07", "00:08"},
	{"00:10", "00:11", "00:12"},
	{"00:14", "00:15", "00:16"},
}
var tier1RemainCandidates = [][]string{
	append(tier0RemainCandidates[0], []string{"00:05", "00:06", "00:07", "00:08"}...),
	append(tier0RemainCandidates[2], []string{"00:13", "00:14", "00:15", "00:16"}...),
}
var tier2Remain = append(tier1RemainCandidates[0],
	[]string{"00:09", "00:10", "00:11", "00:12", "00:13", "00:14", "00:15", "00:16"}...)

func generateTopologyFromDistance(deviceDistances map[string]map[string]int) *pcitopov2.Pcitopo {
	topo := &pcitopov2.Pcitopo{
		NumDevices: len(deviceDistances),
		Devices:    make(map[string]pcitopov2.Device),
	}
	for dev, distanceMap := range deviceDistances {
		if _, found := topo.Devices[dev]; !found {
			topo.Devices[dev] = pcitopov2.Device{
				Name: dev,
				Peers: pcitopov2.Peers{
					Peer0: make(map[string]int),
					Peer1: make(map[string]int),
					Peer2: make(map[string]int),
				},
			}
		}
		for peer, distance := range distanceMap {
			if _, found := topo.Devices[peer]; !found {
				topo.Devices[peer] = pcitopov2.Device{
					Name: peer,
					Peers: pcitopov2.Peers{
						Peer0: make(map[string]int),
						Peer1: make(map[string]int),
						Peer2: make(map[string]int),
					},
				}
			}
			switch distance {
			case zeroDistance:
				topo.Devices[dev].Peers.Peer0[peer] = zeroDistance
				topo.Devices[peer].Peers.Peer0[dev] = zeroDistance
			case oneDistance:
				topo.Devices[dev].Peers.Peer1[peer] = oneDistance
				topo.Devices[peer].Peers.Peer1[dev] = oneDistance
			case twoDistance:
				topo.Devices[dev].Peers.Peer2[peer] = twoDistance
				topo.Devices[peer].Peers.Peer2[dev] = twoDistance
			}
		}
	}
	return topo
}

var _ = Describe("Test Topology", func() {
	deviceMap := make(map[string]*pluginapi.Device)
	for _, dev := range allDevices {
		deviceMap[dev] = &pluginapi.Device{
			ID: dev,
		}
	}
	ctx := context.Background()

	Context("Pseudo topology file", func() {
		It("can unmarshal pseudo topology file in init container", func() {
			pciTopo, err := GetPciTopology(pseudoTopologyFilePath, false)
			Expect(err).To(BeNil())
			Expect(pciTopo.Devices).To(HaveLen(8))
			Expect(pciTopo.SpyreVfDevices).To(HaveLen(8))
		})
	})

	Context("Topology test with pseudoDevice", func() {
		BeforeEach(func() {
			_ = os.Setenv("NODE_NAME", node1Name)
			_ = os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), spyreconst.ModeEnabledValue)
			_ = os.Setenv(spyrev1alpha1.ReservationMode.EnvKey(), spyreconst.ModeEnabledValue)
			PciTopology = generateTopologyFromDistance(deviceDistances)
		})
		AfterEach(func() {
			_ = os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
			_ = os.Unsetenv(spyrev1alpha1.ReservationMode.EnvKey())
			PciTopology = nil
		})
		DescribeTable("GetMaxValidPeers",
			func(resourceSuffix string, selfAllocation map[string]bool, candidateGroup [][]string) {
				resourceName := pfTierResourceName(resourceSuffix)
				maxValidPeers := GetMaxValidPeers(deviceMap, resourceName, selfAllocation)
				numOfAllocated := 0
				for dev, allocated := range selfAllocation {
					if allocated {
						numOfAllocated += 1
						_, found := deviceMap[dev]
						Expect(found).To(BeTrue())
					}
				}
				By("check self allocated must be added")
				for dev, allocated := range selfAllocation {
					if allocated {
						_, found := maxValidPeers[dev]
						Expect(found).To(BeTrue())
					}
				}
				By("check length")
				if len(candidateGroup) == 0 {
					Expect(len(maxValidPeers)).To(Equal(numOfAllocated))
				} else {
					expectedAllocatableLen := len(candidateGroup[0])
					for _, expectedDevices := range candidateGroup {
						Expect(len(expectedDevices)).To(Equal(expectedAllocatableLen))
					}
					Expect(len(maxValidPeers)).To(Equal(expectedAllocatableLen + numOfAllocated))
					By("check next allocatable devices")
					var found bool
					for _, expectedDevices := range candidateGroup {
						foundCount := 0
						for _, dev := range expectedDevices {
							if _, foundOne := maxValidPeers[dev]; foundOne {
								foundCount += 1
							}
						}
						if foundCount == expectedAllocatableLen {
							found = true
							break
						}
					}
					Expect(found).To(BeTrue())
				}
			},

			Entry("tier0, no self allocation", spyreconst.TierZeroResourceNameSuffix, noSelfAllocated,
				[][]string{firstTier0Group, secondTier0Group, thirdTier0Group, fourthTier0Group}),
			Entry("tier0, first allocated", spyreconst.TierZeroResourceNameSuffix, firstTier0SelfAllocated,
				[][]string{secondTier0Group, thirdTier0Group, fourthTier0Group}),
			Entry("tier0, more than one allocated", spyreconst.TierZeroResourceNameSuffix, moreThanOneTier0SelfAllocated,
				[][]string{thirdTier0Group, fourthTier0Group}),
			Entry("tier0, all group allocated but remains", spyreconst.TierZeroResourceNameSuffix, allTier0SelfAllocated,
				tier0RemainCandidates),

			Entry("tier1, no self allocation", spyreconst.TierOneResourceNameSuffix, noSelfAllocated,
				[][]string{firstTier1Group, secondTier1Group}),
			Entry("tier1, first allocated", spyreconst.TierOneResourceNameSuffix, firstTier1SelfAllocated,
				[][]string{secondTier1Group}),
			Entry("tier1, all group allocated but remains", spyreconst.TierOneResourceNameSuffix, allTier1SelfAllocated,
				tier1RemainCandidates),

			Entry("tier2, no self allocation", spyreconst.TierTwoResourceNameSuffix, noSelfAllocated,
				[][]string{allDevices}),
			Entry("tier2, allocated but remains", spyreconst.TierTwoResourceNameSuffix, firstTier0SelfAllocated,
				[][]string{tier2Remain}),
			Entry("tier0, all allocated", spyreconst.TierZeroResourceNameSuffix, allAllocated, [][]string{}),

			Entry("tier1, all allocated", spyreconst.TierZeroResourceNameSuffix, allAllocated, [][]string{}),
			Entry("tier2, all allocated", spyreconst.TierZeroResourceNameSuffix, allAllocated, [][]string{}),
		)

		DescribeTable("Pcitopo.ValidateTier", func(resourceSuffix string, deviceIDs []string, expectedErr error) {
			resourceName := pfTierResourceName(resourceSuffix)
			err := PciTopology.ValidateTier(resourceName, deviceIDs)
			if expectedErr == nil {
				Expect(err).To(BeNil())
			} else {
				Expect(err).NotTo(BeNil())
				Expect(err.Error()).To(BeEquivalentTo(expectedErr.Error()))
			}
		},
			Entry("empty list", spyreconst.TierZeroResourceNameSuffix, []string{}, nil),
			Entry("single member", spyreconst.TierZeroResourceNameSuffix, []string{firstTier0Group[0]}, nil),
			Entry("valid 1st tier0", spyreconst.TierZeroResourceNameSuffix, firstTier0Group, nil),
			Entry("valid 2nd tier0", spyreconst.TierZeroResourceNameSuffix, secondTier0Group, nil),
			Entry("valid 3rd tier0", spyreconst.TierZeroResourceNameSuffix, thirdTier0Group, nil),
			Entry("valid 4th tier0", spyreconst.TierZeroResourceNameSuffix, fourthTier0Group, nil),
			Entry("invalid tier0", spyreconst.TierZeroResourceNameSuffix,
				append(firstTier0Group, secondTier0Group[0]), pcitopov2.OutOfExpectedTierErr),
			Entry("invalid deviceID", spyreconst.TierZeroResourceNameSuffix,
				append([]string{"invalidDevice"}, firstTier0Group...), pcitopov2.DeviceNotFoundErr),
			Entry("valid 1st tier1", spyreconst.TierOneResourceNameSuffix, firstTier1Group, nil),
			Entry("valid 2nd tier1", spyreconst.TierOneResourceNameSuffix, secondTier1Group, nil),
			Entry("invalid tier1", spyreconst.TierOneResourceNameSuffix,
				append(firstTier1Group, secondTier1Group[0]), pcitopov2.OutOfExpectedTierErr),
			Entry("valid tier2", spyreconst.TierTwoResourceNameSuffix, allDevices, nil),
		)

		It("use custom topology", func() {
			originFolder := DefaultTopologyFolder
			DefaultTopologyFolder = topologyFolder
			topologyFile := GetTopologyFile()
			expectedTopologyFile := filepath.Join(topologyFolder, node1Name)
			Expect(topologyFile).To(Equal(expectedTopologyFile))
			_, err := GetPciTopology(topologyFile, false)
			Expect(err).To(BeNil())
			DefaultTopologyFolder = originFolder
		})
	})

	Context("pcitopo integration", func() {
		var spyreClient *spyreclient.SpyreClient
		var err error
		var namespace string
		nodeList := []string{node1Name, node2Name}
		_ = os.Setenv("NODE_NAME", node1Name)

		BeforeEach(func() {
			_ = os.Setenv(spyrev1alpha1.ReservationMode.EnvKey(), spyreconst.ModeEnabledValue)
			spyreClient, err = spyreclient.NewClient(context.Background(), Cfg)
			Expect(err).To(BeNil())
			Expect(spyreClient).NotTo((BeNil()))
			namespace = createNewNamespace(ctx)
			for _, node := range nodeList {
				s := &spyrev1alpha1.SpyreNodeState{
					ObjectMeta: metav1.ObjectMeta{
						Name:      node,
						Namespace: namespace,
					},
					Spec: spyrev1alpha1.SpyreNodeStateSpec{
						NodeName: node,
						SpyreInterfaces: []spyrev1alpha1.SpyreInterface{
							{PciAddress: "00:01", NumVfs: 1},
							{PciAddress: "00:02", NumVfs: 1},
						},
					},
					Status: spyrev1alpha1.SpyreNodeStateStatus{},
				}
				_, err = spyreClient.Create(ctx, s)
				Expect(err).To(BeNil())
			}
			nsList, err := spyreClient.List(ctx, &client.ListOptions{})
			Expect(err).To(BeNil())
			Expect(len(nsList.Items)).Should(Equal(len(nodeList)))
			PciTopoCommand = pcitopoPath
			PciTopology = nil
		})

		AfterEach(func() {
			_ = os.Unsetenv(spyrev1alpha1.ReservationMode.EnvKey())
			spyreClient, err = spyreclient.NewClient(context.Background(), Cfg)
			Expect(err).To(BeNil())
			Expect(spyreClient).NotTo((BeNil()))
			Expect(err).To(BeNil())
			for _, nodeName := range nodeList {
				err = spyreClient.Delete(ctx, nodeName, &client.DeleteOptions{})
				Expect(err).To(BeNil())
			}
			PciTopoCommand = "pcitopo"
		})

	})

	Context("Topology test without pseudoDevice", Ordered, func() {

		var (
			simpleDistance = map[string]map[string]int{"01": {"02": 2, "03": 4, "04": 8},
				"02": {"03": 4, "04": 8},
				"03": {"04": 4},
				"04": {}}
			oneMissing = map[string]map[string]int{"01": {"02": 2, "03": 4},
				"02": {"03": 4},
				"03": {}}
			twoMissing = map[string]map[string]int{"01": {"02": 2},
				"02": {}}
			threeMissing = map[string]map[string]int{"01": {}}
		)

		BeforeAll(func() {
			utils.SysBusPci = devicePath
		})

		AfterAll(func() {
			utils.SysBusPci = "/sys/bus/pci/devices"
		})

		BeforeEach(func() {
			_ = os.Setenv("NODE_NAME", node1Name)
			_ = os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
			err := utils.CreateFolderIfNotExists(devicePath)
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			_ = os.Unsetenv(spyrev1alpha1.ReservationMode.EnvKey())
			PciTopology = nil
			_ = os.RemoveAll(devicePath)
		})

		It("use custom topology config folder", func() {
			originFolder := DefaultTopologyFolder
			DefaultTopologyFolder = topologyFolder
			expectedTopologyFile := filepath.Join(topologyFolder, node1Name)
			Expect(utils.PathExists(expectedTopologyFile)).To(BeTrue())
			topologyFile := GetTopologyFile()
			Expect(topologyFile).To(Equal(expectedTopologyFile))
			_, err := GetPciTopology(topologyFile, false)
			Expect(err).To(BeNil())
			DefaultTopologyFolder = originFolder
		})

		It("set and use metadata topology file", func() {
			Expect(utils.PathExists(topoV2FilePath)).To(BeTrue())
			originTopofile := MetadataTopologyFilepath
			MetadataTopologyFilepath = topoV2FilePath
			topologyFile := GetTopologyFile()
			Expect(topologyFile).To(Equal(topoV2FilePath))
			MetadataTopologyFilepath = originTopofile
		})

		It("GetOriginalTopologyFile returns metadata file when present", func() {
			// preserve and restore globals
			origMetaTopo := MetadataTopologyFilepath
			origIgnore := os.Getenv(IgnoreMetadataKey)
			defer func() {
				MetadataTopologyFilepath = origMetaTopo
				if origIgnore == "" {
					_ = os.Unsetenv(IgnoreMetadataKey)
				} else {
					_ = os.Setenv(IgnoreMetadataKey, origIgnore)
				}
			}()

			tmpDir, err := os.MkdirTemp("", "topo-meta-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			tmpFile := filepath.Join(tmpDir, "topo.json")
			Expect(os.WriteFile(tmpFile, []byte("{}"), 0644)).To(BeNil())

			MetadataTopologyFilepath = tmpFile
			_ = os.Unsetenv(IgnoreMetadataKey)

			got := GetOriginalTopologyFile()
			Expect(got).To(Equal(tmpFile))
		})

		It("GetOriginalTopologyFile returns configmap file when metadata missing", func() {
			origMetaTopo := MetadataTopologyFilepath
			origDefaultFolder := DefaultTopologyFolder
			origNode := os.Getenv("NODE_NAME")
			defer func() {
				MetadataTopologyFilepath = origMetaTopo
				DefaultTopologyFolder = origDefaultFolder
				if origNode == "" {
					_ = os.Unsetenv("NODE_NAME")
				} else {
					_ = os.Setenv("NODE_NAME", origNode)
				}
			}()
			MetadataTopologyFilepath = filepath.Join(os.TempDir(), "non-existent-topo.json")

			tmpDir, err := os.MkdirTemp("", "topo-configmap-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			nodeName := "node-test-x"
			_ = os.Setenv("NODE_NAME", nodeName)
			DefaultTopologyFolder = tmpDir

			expected := filepath.Join(tmpDir, nodeName)
			Expect(os.WriteFile(expected, []byte("{}"), 0644)).To(BeNil())

			got := GetOriginalTopologyFile()
			Expect(got).To(Equal(expected))
		})

		It("GetOriginalTopologyFile returns empty when no topology exists", func() {
			origMetaTopo := MetadataTopologyFilepath
			origDefaultFolder := DefaultTopologyFolder
			origNode := os.Getenv("NODE_NAME")
			origIgnore := os.Getenv(IgnoreMetadataKey)
			defer func() {
				MetadataTopologyFilepath = origMetaTopo
				DefaultTopologyFolder = origDefaultFolder
				if origNode == "" {
					_ = os.Unsetenv("NODE_NAME")
				} else {
					_ = os.Setenv("NODE_NAME", origNode)
				}
				if origIgnore == "" {
					_ = os.Unsetenv(IgnoreMetadataKey)
				} else {
					_ = os.Setenv(IgnoreMetadataKey, origIgnore)
				}
			}()
			MetadataTopologyFilepath = filepath.Join(os.TempDir(), "non-existent-topo.json")

			tmpDir, err := os.MkdirTemp("", "topo-empty-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			nodeName := "node-test-y"
			_ = os.Setenv("NODE_NAME", nodeName)
			DefaultTopologyFolder = tmpDir
			_ = os.Unsetenv(IgnoreMetadataKey)

			got := GetOriginalTopologyFile()
			Expect(got).To(Equal(""))
		})

		It("GetCurrentTopologyFile returns dynamic file when it exists", func() {
			origDynamicFile := DynamicTopologyFilepath
			origMetaTopo := MetadataTopologyFilepath
			defer func() {
				DynamicTopologyFilepath = origDynamicFile
				MetadataTopologyFilepath = origMetaTopo
			}()

			tmpDir, err := os.MkdirTemp("", "topo-dynamic-current-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()
			dynamicFile := filepath.Join(tmpDir, "topo.json")
			Expect(os.WriteFile(dynamicFile, []byte("{}"), 0644)).To(BeNil())
			DynamicTopologyFilepath = dynamicFile
			tmpMeta := filepath.Join(tmpDir, "metadata-topo.json")
			Expect(os.WriteFile(tmpMeta, []byte("{}"), 0644)).To(BeNil())
			MetadataTopologyFilepath = tmpMeta
			got := GetCurrentTopologyFile()
			Expect(got).To(Equal(dynamicFile))
		})

		It("GetCurrentTopologyFile falls back to metadata when dynamic missing", func() {
			origDynamicFile := DynamicTopologyFilepath
			origMetaTopo := MetadataTopologyFilepath
			origIgnore := os.Getenv(IgnoreMetadataKey)
			defer func() {
				DynamicTopologyFilepath = origDynamicFile
				MetadataTopologyFilepath = origMetaTopo
				if origIgnore == "" {
					_ = os.Unsetenv(IgnoreMetadataKey)
				} else {
					_ = os.Setenv(IgnoreMetadataKey, origIgnore)
				}
			}()

			tmpDir, err := os.MkdirTemp("", "topo-no-dynamic-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			DynamicTopologyFilepath = filepath.Join(tmpDir, "non-existent-dynamic.json")

			tmpMeta := filepath.Join(tmpDir, "metadata-topo.json")
			Expect(os.WriteFile(tmpMeta, []byte("{}"), 0644)).To(BeNil())
			MetadataTopologyFilepath = tmpMeta
			_ = os.Unsetenv(IgnoreMetadataKey)

			got := GetCurrentTopologyFile()
			Expect(got).To(Equal(tmpMeta))
		})

		It("SaveDynamicTopology creates folder and writes file", func() {
			origDynamicFile := DynamicTopologyFilepath
			defer func() {
				DynamicTopologyFilepath = origDynamicFile
			}()

			tmpDir, err := os.MkdirTemp("", "topo-save-dynamic-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			dynFolder := filepath.Join(tmpDir, "dynamic")
			dynFile := filepath.Join(dynFolder, "topo.json")
			DynamicTopologyFilepath = dynFile

			testTopo := pcitopov2.Pcitopo{
				Devices:    map[string]pcitopov2.Device{"0000": {}},
				Version:    2.0,
				NumDevices: 1,
			}

			err = SaveDynamicTopology(testTopo)
			Expect(err).To(BeNil())
			Expect(utils.PathExists(dynFolder)).To(BeTrue())
			Expect(utils.PathExists(dynFile)).To(BeTrue())
			data, err := os.ReadFile(dynFile)
			Expect(err).To(BeNil())
			Expect(len(data)).To(BeNumerically(">", 0))
			readTopo, err := pcitopov2.UnmarshalPciTopo(data)
			Expect(err).To(BeNil())
			Expect(readTopo.NumDevices).To(Equal(1))
		})

		It("EnsureDynamicTopologyFiltered returns nil when dynamic file already exists", func() {
			origDynamicFile := DynamicTopologyFilepath
			defer func() {
				DynamicTopologyFilepath = origDynamicFile
			}()

			tmpDir, err := os.MkdirTemp("", "topo-ensure-exists-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			dynFile := filepath.Join(tmpDir, "topo.json")
			DynamicTopologyFilepath = dynFile
			Expect(os.WriteFile(dynFile, []byte("{}"), 0644)).To(BeNil())
			err = EnsureDynamicTopologyFiltered()
			Expect(err).To(BeNil())
		})

		It("EnsureDynamicTopologyFiltered creates dynamic file when missing", func() {
			origDynamicFile := DynamicTopologyFilepath
			origMetaTopo := MetadataTopologyFilepath
			defer func() {
				DynamicTopologyFilepath = origDynamicFile
				MetadataTopologyFilepath = origMetaTopo
			}()

			tmpDir, err := os.MkdirTemp("", "topo-ensure-create-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()
			dynFile := filepath.Join(tmpDir, "dynamic", "topo.json")
			metaFile := filepath.Join(tmpDir, "metadata-topo.json")
			DynamicTopologyFilepath = dynFile
			MetadataTopologyFilepath = metaFile
			testTopo := pcitopov2.Pcitopo{
				Devices:    map[string]pcitopov2.Device{"0001": {}, "0002": {}},
				Version:    2.0,
				NumDevices: 2,
			}
			Expect(os.WriteFile(metaFile, []byte(testTopo.String()), 0644)).To(BeNil())
			err = EnsureDynamicTopologyFiltered()
			Expect(err).To(BeNil())
			Expect(utils.PathExists(dynFile)).To(BeTrue())
		})

		It("EnsureDynamicTopologyFiltered returns error when no original topology exists", func() {
			origDynamicFile := DynamicTopologyFilepath
			origMetaTopo := MetadataTopologyFilepath
			origDefaultFolder := DefaultTopologyFolder
			defer func() {
				DynamicTopologyFilepath = origDynamicFile
				MetadataTopologyFilepath = origMetaTopo
				DefaultTopologyFolder = origDefaultFolder
			}()

			tmpDir, err := os.MkdirTemp("", "topo-ensure-nooriginal-")
			Expect(err).To(BeNil())
			defer func() { _ = os.RemoveAll(tmpDir) }()
			// Setup paths to non-existent files
			DynamicTopologyFilepath = filepath.Join(tmpDir, "dynamic", "topo.json")
			MetadataTopologyFilepath = filepath.Join(tmpDir, "non-existent-meta.json")
			DefaultTopologyFolder = filepath.Join(tmpDir, "non-existent-folder")
			err = EnsureDynamicTopologyFiltered()
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("no original topology file available"))
		})

		It("skip metadata topology file", func() {
			Expect(utils.PathExists(topoV2FilePath)).To(BeTrue())
			originTopofile := MetadataTopologyFilepath
			MetadataTopologyFilepath = topoV2FilePath
			_ = os.Setenv(IgnoreMetadataKey, "true")
			topologyFile := GetTopologyFile()
			// After removing pcitopo, expect empty string (topology should come from init container)
			Expect(topologyFile).To(Equal(""))
			MetadataTopologyFilepath = originTopofile
			_ = os.Unsetenv(IgnoreMetadataKey)
		})

		DescribeTable("can filter out missing devices", func(deviceList []string,
			expectedOutput map[string]map[string]int) {
			originalPciTopo := generateTopologyFromDistance(simpleDistance)
			expectedPcitopo := generateTopologyFromDistance(expectedOutput)
			createDevicesInTestDevicePath(deviceList)
			output := FilterOutMissingDeviceFromTopology(*originalPciTopo)
			Expect(output.NumDevices).To(Equal(expectedPcitopo.NumDevices))
			for name, device := range output.Devices {
				expectedDevice, found := expectedPcitopo.Devices[name]
				Expect(found).To(BeTrue(), fmt.Sprintf("%s must found\n%v\n%v",
					name, output.Devices, expectedPcitopo.Devices))
				Expect(len(device.Peers.Peer0)).To(Equal(len(expectedDevice.Peers.Peer0)))
				for peer := range device.Peers.Peer0 {
					_, found := expectedDevice.Peers.Peer0[peer]
					Expect(found).To(BeTrue(), fmt.Sprintf("%s must found", peer))
				}
				Expect(len(device.Peers.Peer1)).To(Equal(len(expectedDevice.Peers.Peer1)))
				for peer := range device.Peers.Peer1 {
					_, found := expectedDevice.Peers.Peer1[peer]
					Expect(found).To(BeTrue(), fmt.Sprintf("%s must found", peer))
				}
				Expect(len(device.Peers.Peer2)).To(Equal(len(expectedDevice.Peers.Peer2)))
				for peer := range device.Peers.Peer2 {
					_, found := expectedDevice.Peers.Peer2[peer]
					Expect(found).To(BeTrue(), fmt.Sprintf("%s must found", peer))
				}
			}
		},
			Entry("no missing", []string{"01", "02", "03", "04"}, simpleDistance),
			Entry("one missing", []string{"01", "02", "03"}, oneMissing),
			Entry("two missing", []string{"01", "02"}, twoMissing),
			Entry("three missing", []string{"01"}, threeMissing),
			Entry("all missing", []string{}, map[string]map[string]int{}),
		)

		It("filterOutMissingDeviceFromTopology cleans up device peers", func() {
			testTopo := pcitopov2.Pcitopo{
				NumDevices:        2,
				SpyreVfNumDevices: 2,
				Devices: map[string]pcitopov2.Device{
					"0001:00:00.0": {
						Name:     "Device1",
						DeviceId: "06a7",
						Peers: pcitopov2.Peers{
							Peer0: map[string]int{"0002:00:00.0": 100},
						},
					},
					"0002:00:00.0": {
						Name:     "Device2",
						DeviceId: "06a7",
						Peers: pcitopov2.Peers{
							Peer0: map[string]int{"0001:00:00.0": 100},
						},
					},
				},
				SpyreVfDevices: map[string]pcitopov2.Device{
					"0003:00:00.1": {
						Name:     "VFDevice1",
						DeviceId: "06a8",
						SpyreVfPeers: pcitopov2.Peers{
							Peer0: map[string]int{"0004:00:00.1": 50},
							Peer1: map[string]int{"0002:00:00.0": 200},
							Peer2: map[string]int{"0001:00:00.0": 300},
						},
					},
					"0004:00:00.1": {
						Name:     "VFDevice2",
						DeviceId: "06a8",
						SpyreVfPeers: pcitopov2.Peers{
							Peer0: map[string]int{"0003:00:00.1": 50},
							Peer1: map[string]int{"0001:00:00.0": 200},
						},
					},
				},
			}
			createDevicesInTestDevicePath([]string{"0001:00:00.0", "0003:00:00.1"})
			filtered := FilterOutMissingDeviceFromTopology(testTopo)
			Expect(filtered.NumDevices).To(Equal(1))
			Expect(filtered.SpyreVfNumDevices).To(Equal(1))
			Expect(filtered.Devices).To(HaveKey("0001:00:00.0"))
			Expect(filtered.Devices["0001:00:00.0"].Peers.Peer0).NotTo(HaveKey("0002:00:00.0"))
			Expect(filtered.SpyreVfDevices).To(HaveKey("0003:00:00.1"))
			vfDevice := filtered.SpyreVfDevices["0003:00:00.1"]
			Expect(vfDevice.SpyreVfPeers.Peer0).NotTo(HaveKey("0004:00:00.1"))
			Expect(vfDevice.SpyreVfPeers.Peer1).NotTo(HaveKey("0002:00:00.0"))
			Expect(vfDevice.SpyreVfPeers.Peer2).To(HaveKey("0001:00:00.0"))
		})

		It("filterOutMissingDeviceFromTopology handles nil peer maps", func() {
			testTopo := pcitopov2.Pcitopo{
				NumDevices: 1,
				Devices: map[string]pcitopov2.Device{
					"0001:00:00.0": {
						Name:     "Device1",
						DeviceId: "06a7",
						Peers: pcitopov2.Peers{
							Peer0: nil,
							Peer1: nil,
							Peer2: nil,
						},
					},
				},
				SpyreVfDevices: map[string]pcitopov2.Device{
					"0003:00:00.1": {
						Name:     "VFDevice1",
						DeviceId: "06a8",
						SpyreVfPeers: pcitopov2.Peers{
							Peer0: nil,
							Peer1: nil,
							Peer2: nil,
						},
					},
				},
			}

			createDevicesInTestDevicePath([]string{"0001:00:00.0", "0003:00:00.1"})
			filtered := FilterOutMissingDeviceFromTopology(testTopo)
			Expect(filtered.NumDevices).To(Equal(1))
			Expect(filtered.SpyreVfNumDevices).To(Equal(1))
		})

		It("can filter out from GetPciTopology", func() {
			deviceList := []string{"0000:29:00.0"}
			createDevicesInTestDevicePath(deviceList)
			output, err := GetPciTopology(topoV2FilePath, true)
			Expect(err).To(BeNil())
			// After filtering, should have only the devices that exist in device path
			// The test creates only 0000:29:00.0, so expect 1 device
			Expect(output.NumDevices).To(Equal(1))
			Expect(output.Devices).To(HaveLen(1))
			// Check that the device exists and has filtered peers
			Expect(output.Devices).To(HaveKey(deviceList[0]))
			Expect(output.Devices[deviceList[0]].Peers.Peer0).To(HaveLen(0))
			Expect(output.Devices[deviceList[0]].Peers.Peer1).To(HaveLen(0))
			Expect(output.Devices[deviceList[0]].Peers.Peer2).To(HaveLen(0))
		})
	})

	Context("Topology test without configmap", func() {
		BeforeEach(func() {
			if utils.PathExists(DynamicTopologyFilepath) {
				_ = os.Remove(DynamicTopologyFilepath)
			}
		})

		AfterEach(func() {
			// reset global variable after each test
			PciTopology = nil
			if utils.PathExists(DynamicTopologyFilepath) {
				_ = os.Remove(DynamicTopologyFilepath)
			}
		})

		DescribeTable("GetPciTopology", func(topologyFilePath string, expectedErr bool) {
			_, err := GetPciTopology(topologyFilePath, false)
			Expect(err != nil).To(Equal(expectedErr))
			if expectedErr {
				Expect(err.Error()).To(BeEquivalentTo("failed to get topology file"))
				Expect(PciTopology).To(BeNil())
			}
		},
			Entry("topology file given", topoV2FilePath, false),
			Entry("topology file not given", "", true),
		)
	})

	DescribeTable("FilterTopologyByDeviceHealth", func(
		originalDevices map[string]pcitopov2.Device,
		originalVfDevices map[string]pcitopov2.Device,
		healthyDevices map[string]bool,
		expectedDeviceCount int,
		expectedVfDeviceCount int,
		expectedDevices []string,
		expectedVfDevices []string,
	) {
		originalTopo := pcitopov2.Pcitopo{
			NumDevices:        len(originalDevices),
			SpyreVfNumDevices: len(originalVfDevices),
			Devices:           originalDevices,
			SpyreVfDevices:    originalVfDevices,
		}
		filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
		Expect(filteredTopo.NumDevices).To(Equal(expectedDeviceCount))
		Expect(filteredTopo.SpyreVfNumDevices).To(Equal(expectedVfDeviceCount))
		Expect(len(filteredTopo.Devices)).To(Equal(expectedDeviceCount))
		Expect(len(filteredTopo.SpyreVfDevices)).To(Equal(expectedVfDeviceCount))
		for _, deviceAddr := range expectedDevices {
			Expect(filteredTopo.Devices).To(HaveKey(deviceAddr))
		}
		for _, vfDeviceAddr := range expectedVfDevices {
			Expect(filteredTopo.SpyreVfDevices).To(HaveKey(vfDeviceAddr))
		}
		for deviceAddr := range originalDevices {
			if !healthyDevices[deviceAddr] {
				Expect(filteredTopo.Devices).NotTo(HaveKey(deviceAddr))
			}
		}
		for vfDeviceAddr := range originalVfDevices {
			if !healthyDevices[vfDeviceAddr] {
				Expect(filteredTopo.SpyreVfDevices).NotTo(HaveKey(vfDeviceAddr))
			}
		}
	},
		Entry("all devices healthy",
			map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
				"0002:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
			},
			map[string]pcitopov2.Device{
				"0003:00:00.0": {DeviceId: "06a8", Name: "Spyre VF Device"},
				"0004:00:00.0": {DeviceId: "06a8", Name: "Spyre VF Device"},
			},
			map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": true,
				"0003:00:00.0": true,
				"0004:00:00.0": true,
			},
			2, 2,
			[]string{"0001:00:00.0", "0002:00:00.0"},
			[]string{"0003:00:00.0", "0004:00:00.0"},
		),
		Entry("some devices unhealthy",
			map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
				"0002:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
			},
			map[string]pcitopov2.Device{
				"0003:00:00.0": {DeviceId: "06a8", Name: "Spyre VF Device"},
				"0004:00:00.0": {DeviceId: "06a8", Name: "Spyre VF Device"},
			},
			map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": false,
				"0003:00:00.0": true,
				"0004:00:00.0": false,
			},
			1, 1,
			[]string{"0001:00:00.0"},
			[]string{"0003:00:00.0"},
		),
		Entry("all devices unhealthy",
			map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
				"0002:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
			},
			map[string]pcitopov2.Device{
				"0003:00:00.0": {DeviceId: "06a8", Name: "Spyre VF Device"},
				"0004:00:00.0": {DeviceId: "06a8", Name: "Spyre VF Device"},
			},
			map[string]bool{
				"0001:00:00.0": false,
				"0002:00:00.0": false,
				"0003:00:00.0": false,
				"0004:00:00.0": false,
			},
			0, 0,
			[]string{},
			[]string{},
		),
		Entry("device restoration from original",
			map[string]pcitopov2.Device{
				"0001:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
				"0002:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"},
				"0005:00:00.0": {DeviceId: "06a8", Name: "Spyre Device"}, // This device was missing but now healthy
			},
			map[string]pcitopov2.Device{},
			map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": false,
				"0005:00:00.0": true, // Previously missing device now healthy
			},
			2, 0,
			[]string{"0001:00:00.0", "0005:00:00.0"},
			[]string{},
		),
		Entry("empty topology",
			map[string]pcitopov2.Device{},
			map[string]pcitopov2.Device{},
			map[string]bool{},
			0, 0,
			[]string{},
			[]string{},
		),
	)

	Context("FilterTopologyByDeviceHealth with peer cleanup", func() {
		It("should clean up peer references for unhealthy devices", func() {
			originalDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {
					DeviceId: "06a8",
					Name:     "Spyre Device",
					Peers: pcitopov2.Peers{
						Peer0: map[string]int{"0002:00:00.0": 100},
						Peer1: map[string]int{"0003:00:00.0": 200},
						Peer2: map[string]int{"0004:00:00.0": 300},
					},
				},
				"0002:00:00.0": {
					DeviceId: "06a8",
					Name:     "Spyre Device",
					Peers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.0": 100},
						Peer1: map[string]int{"0003:00:00.0": 150},
					},
				},
				"0003:00:00.0": {
					DeviceId: "06a8",
					Name:     "Spyre Device",
					Peers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.0": 200, "0002:00:00.0": 150},
					},
				},
				"0004:00:00.0": {
					DeviceId: "06a8",
					Name:     "Spyre Device",
					Peers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.0": 300},
					},
				},
			}

			originalTopo := pcitopov2.Pcitopo{
				NumDevices:     len(originalDevices),
				Devices:        originalDevices,
				SpyreVfDevices: make(map[string]pcitopov2.Device),
			}

			healthyDevices := map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": false,
				"0003:00:00.0": true,
				"0004:00:00.0": false,
			}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			Expect(filteredTopo.Devices).To(HaveKey("0001:00:00.0"))
			Expect(filteredTopo.Devices).To(HaveKey("0003:00:00.0"))
			Expect(filteredTopo.Devices).NotTo(HaveKey("0002:00:00.0"))
			Expect(filteredTopo.Devices).NotTo(HaveKey("0004:00:00.0"))
			device1 := filteredTopo.Devices["0001:00:00.0"]
			Expect(device1.Peers.Peer0).NotTo(HaveKey("0002:00:00.0"))
			Expect(device1.Peers.Peer1).To(HaveKey("0003:00:00.0"))
			Expect(device1.Peers.Peer2).NotTo(HaveKey("0004:00:00.0"))

			device3 := filteredTopo.Devices["0003:00:00.0"]
			Expect(device3.Peers.Peer0).To(HaveKey("0001:00:00.0"))
			Expect(device3.Peers.Peer0).NotTo(HaveKey("0002:00:00.0"))
		})

		It("should handle VF devices filtering correctly", func() {
			originalVfDevices := map[string]pcitopov2.Device{
				"0001:00:00.1": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device",
					SpyreVfPeers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.2": 100},
					},
				},
				"0001:00:00.2": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device",
					SpyreVfPeers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.1": 100},
					},
				},
			}

			originalTopo := pcitopov2.Pcitopo{
				SpyreVfNumDevices: len(originalVfDevices),
				SpyreVfDevices:    originalVfDevices,
				Devices:           make(map[string]pcitopov2.Device),
			}

			healthyDevices := map[string]bool{
				"0001:00:00.1": true,
				"0001:00:00.2": false,
			}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			Expect(filteredTopo.SpyreVfDevices).To(HaveKey("0001:00:00.1"))
			Expect(filteredTopo.SpyreVfDevices).NotTo(HaveKey("0001:00:00.2"))
			Expect(filteredTopo.SpyreVfNumDevices).To(Equal(1))

			vf1 := filteredTopo.SpyreVfDevices["0001:00:00.1"]
			Expect(vf1.SpyreVfPeers.Peer0).NotTo(HaveKey("0001:00:00.2"))
		})

		It("should clean up VF Peer1 references to unhealthy devices", func() {
			originalVfDevices := map[string]pcitopov2.Device{
				"0001:00:00.1": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 1",
					SpyreVfPeers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.2": 100},
						Peer1: map[string]int{
							"0001:00:00.3": 200,
							"0001:00:00.4": 200,
						},
					},
				},
				"0001:00:00.2": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 2",
				},
				"0001:00:00.3": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 3",
				},
				"0001:00:00.4": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 4",
				},
			}

			originalTopo := pcitopov2.Pcitopo{
				SpyreVfNumDevices: len(originalVfDevices),
				SpyreVfDevices:    originalVfDevices,
				Devices:           make(map[string]pcitopov2.Device),
			}

			healthyDevices := map[string]bool{
				"0001:00:00.1": true,
				"0001:00:00.2": true,
				"0001:00:00.3": true,
				"0001:00:00.4": false,
			}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			vf1 := filteredTopo.SpyreVfDevices["0001:00:00.1"]

			Expect(vf1.SpyreVfPeers.Peer0).To(HaveKey("0001:00:00.2"))
			Expect(vf1.SpyreVfPeers.Peer1).To(HaveKey("0001:00:00.3"))
			Expect(vf1.SpyreVfPeers.Peer1).NotTo(HaveKey("0001:00:00.4"))
		})

		It("should clean up VF Peer2 references to unhealthy devices", func() {
			originalVfDevices := map[string]pcitopov2.Device{
				"0001:00:00.1": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 1",
					SpyreVfPeers: pcitopov2.Peers{
						Peer0: map[string]int{"0001:00:00.2": 100},
						Peer1: map[string]int{"0001:00:00.3": 200},
						Peer2: map[string]int{
							"0001:00:00.4": 300,
							"0001:00:00.5": 300,
						},
					},
				},
				"0001:00:00.2": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 2",
				},
				"0001:00:00.3": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 3",
				},
				"0001:00:00.4": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 4",
				},
				"0001:00:00.5": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 5",
				},
			}

			originalTopo := pcitopov2.Pcitopo{
				SpyreVfNumDevices: len(originalVfDevices),
				SpyreVfDevices:    originalVfDevices,
				Devices:           make(map[string]pcitopov2.Device),
			}

			healthyDevices := map[string]bool{
				"0001:00:00.1": true,
				"0001:00:00.2": true,
				"0001:00:00.3": true,
				"0001:00:00.4": false,
				"0001:00:00.5": true,
			}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			vf1 := filteredTopo.SpyreVfDevices["0001:00:00.1"]

			Expect(vf1.SpyreVfPeers.Peer0).To(HaveKey("0001:00:00.2"))
			Expect(vf1.SpyreVfPeers.Peer1).To(HaveKey("0001:00:00.3"))
			Expect(vf1.SpyreVfPeers.Peer2).NotTo(HaveKey("0001:00:00.4"))
			Expect(vf1.SpyreVfPeers.Peer2).To(HaveKey("0001:00:00.5"))
		})

		It("should handle VF devices with all peer tiers correctly", func() {
			originalVfDevices := map[string]pcitopov2.Device{
				"0001:00:00.1": {
					DeviceId: "06a9",
					Name:     "Spyre VF Device 1",
					SpyreVfPeers: pcitopov2.Peers{
						Peer0: map[string]int{
							"0001:00:00.2": 100,
							"0001:00:00.3": 100,
						},
						Peer1: map[string]int{
							"0001:00:00.4": 200,
							"0001:00:00.5": 200,
						},
						Peer2: map[string]int{
							"0001:00:00.6": 300,
							"0001:00:00.7": 300,
						},
					},
				},
				"0001:00:00.2": {DeviceId: "06a9", Name: "VF 2"},
				"0001:00:00.3": {DeviceId: "06a9", Name: "VF 3"},
				"0001:00:00.4": {DeviceId: "06a9", Name: "VF 4"},
				"0001:00:00.5": {DeviceId: "06a9", Name: "VF 5"},
				"0001:00:00.6": {DeviceId: "06a9", Name: "VF 6"},
				"0001:00:00.7": {DeviceId: "06a9", Name: "VF 7"},
			}

			originalTopo := pcitopov2.Pcitopo{
				SpyreVfNumDevices: len(originalVfDevices),
				SpyreVfDevices:    originalVfDevices,
				Devices:           make(map[string]pcitopov2.Device),
			}

			healthyDevices := map[string]bool{
				"0001:00:00.1": true,
				"0001:00:00.2": true,
				"0001:00:00.3": false,
				"0001:00:00.4": true,
				"0001:00:00.5": false,
				"0001:00:00.6": false,
				"0001:00:00.7": true,
			}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			vf1 := filteredTopo.SpyreVfDevices["0001:00:00.1"]

			Expect(vf1.SpyreVfPeers.Peer0).To(HaveKey("0001:00:00.2"))
			Expect(vf1.SpyreVfPeers.Peer0).NotTo(HaveKey("0001:00:00.3"))
			Expect(vf1.SpyreVfPeers.Peer1).To(HaveKey("0001:00:00.4"))
			Expect(vf1.SpyreVfPeers.Peer1).NotTo(HaveKey("0001:00:00.5"))
			Expect(vf1.SpyreVfPeers.Peer2).NotTo(HaveKey("0001:00:00.6"))
			Expect(vf1.SpyreVfPeers.Peer2).To(HaveKey("0001:00:00.7"))
		})

		It("should handle empty health map by removing all devices", func() {
			originalDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {
					DeviceId: "06a7",
					Name:     "Spyre Device",
				},
			}

			originalTopo := pcitopov2.Pcitopo{
				NumDevices: 1,
				Devices:    originalDevices,
			}

			healthyDevices := map[string]bool{}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			Expect(filteredTopo.Devices).To(BeEmpty())
			Expect(filteredTopo.NumDevices).To(Equal(0))
		})

		It("should preserve all devices when all are healthy", func() {
			originalDevices := map[string]pcitopov2.Device{
				"0001:00:00.0": {
					DeviceId: "06a7",
					Name:     "Spyre Device 1",
				},
				"0002:00:00.0": {
					DeviceId: "06a7",
					Name:     "Spyre Device 2",
				},
			}

			originalTopo := pcitopov2.Pcitopo{
				NumDevices: 2,
				Devices:    originalDevices,
			}

			healthyDevices := map[string]bool{
				"0001:00:00.0": true,
				"0002:00:00.0": true,
			}

			filteredTopo := FilterTopologyByDeviceHealth(originalTopo, healthyDevices)
			Expect(filteredTopo.Devices).To(HaveLen(2))
			Expect(filteredTopo.NumDevices).To(Equal(2))
		})
	})

	Describe("SaveDynamicTopology", func() {
		var tempDir string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "dynamic-topo-test-*")
			Expect(err).NotTo(HaveOccurred())
			DynamicTopologyFilepath = filepath.Join(tempDir, "topo.json")
		})

		AfterEach(func() {
			_ = os.RemoveAll(tempDir)
		})

		It("should create dynamic topology file with correct content", func() {
			testTopo := pcitopov2.Pcitopo{
				Version:    2,
				NumDevices: 1,
				Devices: map[string]pcitopov2.Device{
					"0001:00:00.0": {
						DeviceId: "06a7",
						Name:     "Test Device",
					},
				},
			}

			err := SaveDynamicTopology(testTopo)
			Expect(err).NotTo(HaveOccurred())

			Expect(DynamicTopologyFilepath).To(BeARegularFile())

			data, err := os.ReadFile(DynamicTopologyFilepath)
			Expect(err).NotTo(HaveOccurred())

			readTopo, err := pcitopov2.UnmarshalPciTopo(data)
			Expect(err).NotTo(HaveOccurred())
			Expect(readTopo.NumDevices).To(Equal(1))
			Expect(readTopo.Devices).To(HaveKey("0001:00:00.0"))
		})

		It("should create parent directory if it doesn't exist", func() {
			nestedPath := filepath.Join(tempDir, "nested", "path", "topo.json")
			DynamicTopologyFilepath = nestedPath

			testTopo := pcitopov2.Pcitopo{
				Version:    2,
				NumDevices: 0,
			}

			err := SaveDynamicTopology(testTopo)
			Expect(err).NotTo(HaveOccurred())
			Expect(nestedPath).To(BeARegularFile())
		})

		It("should overwrite existing dynamic topology file", func() {
			firstTopo := pcitopov2.Pcitopo{
				Version:    2,
				NumDevices: 1,
			}

			err := SaveDynamicTopology(firstTopo)
			Expect(err).NotTo(HaveOccurred())

			secondTopo := pcitopov2.Pcitopo{
				Version:    2,
				NumDevices: 2,
			}

			err = SaveDynamicTopology(secondTopo)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(DynamicTopologyFilepath)
			Expect(err).NotTo(HaveOccurred())

			readTopo, err := pcitopov2.UnmarshalPciTopo(data)
			Expect(err).NotTo(HaveOccurred())
			Expect(readTopo.NumDevices).To(Equal(2))
		})
	})

	Describe("GetOriginalTopologyFile and GetCurrentTopologyFile", func() {
		var tempDir string
		var originalMetadataPath string
		var originalDynamicPath string
		var originalDefaultFolder string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "topo-paths-test-*")
			Expect(err).NotTo(HaveOccurred())

			originalMetadataPath = MetadataTopologyFilepath
			originalDynamicPath = DynamicTopologyFilepath
			originalDefaultFolder = DefaultTopologyFolder

			MetadataTopologyFilepath = filepath.Join(tempDir, "metadata", "topo.json")
			DynamicTopologyFilepath = filepath.Join(tempDir, "dynamic", "topo.json")
			DefaultTopologyFolder = filepath.Join(tempDir, "topology")

			_ = os.Setenv(IgnoreMetadataKey, "false")
		})

		AfterEach(func() {
			_ = os.RemoveAll(tempDir)
			MetadataTopologyFilepath = originalMetadataPath
			DynamicTopologyFilepath = originalDynamicPath
			DefaultTopologyFolder = originalDefaultFolder
			_ = os.Unsetenv(IgnoreMetadataKey)
		})

		It("should return metadata path when it exists", func() {
			err := os.MkdirAll(filepath.Dir(MetadataTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(MetadataTopologyFilepath, []byte("test"), 0644)
			Expect(err).NotTo(HaveOccurred())

			path := GetOriginalTopologyFile()
			Expect(path).To(Equal(MetadataTopologyFilepath))
		})

		It("should return empty string when no topology file exists", func() {
			path := GetOriginalTopologyFile()
			Expect(path).To(Equal(""))
		})

		It("should return dynamic path when it exists", func() {
			err := os.MkdirAll(filepath.Dir(DynamicTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(DynamicTopologyFilepath, []byte("dynamic"), 0644)
			Expect(err).NotTo(HaveOccurred())

			path := GetCurrentTopologyFile()
			Expect(path).To(Equal(DynamicTopologyFilepath))
		})

		It("should fall back to original when dynamic doesn't exist", func() {
			err := os.MkdirAll(filepath.Dir(MetadataTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(MetadataTopologyFilepath, []byte("original"), 0644)
			Expect(err).NotTo(HaveOccurred())

			path := GetCurrentTopologyFile()
			Expect(path).To(Equal(MetadataTopologyFilepath))
		})

		It("should prefer dynamic over original when both exist", func() {
			err := os.MkdirAll(filepath.Dir(MetadataTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(MetadataTopologyFilepath, []byte("original"), 0644)
			Expect(err).NotTo(HaveOccurred())

			err = os.MkdirAll(filepath.Dir(DynamicTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(DynamicTopologyFilepath, []byte("dynamic"), 0644)
			Expect(err).NotTo(HaveOccurred())

			currentPath := GetCurrentTopologyFile()
			Expect(currentPath).To(Equal(DynamicTopologyFilepath))

			originalPath := GetOriginalTopologyFile()
			Expect(originalPath).To(Equal(MetadataTopologyFilepath))
		})

		It("should respect IGNORE_EXTERNAL_METADATA flag", func() {
			_ = os.Setenv(IgnoreMetadataKey, "true")

			err := os.MkdirAll(filepath.Dir(MetadataTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(MetadataTopologyFilepath, []byte("metadata"), 0644)
			Expect(err).NotTo(HaveOccurred())

			path := GetOriginalTopologyFile()
			Expect(path).NotTo(Equal(MetadataTopologyFilepath))
		})
	})

	Describe("EnsureDynamicTopologyFiltered", func() {
		var tempDir string
		var originalMetadataPath string
		var originalDynamicPath string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "ensure-dynamic-test-*")
			Expect(err).NotTo(HaveOccurred())

			originalMetadataPath = MetadataTopologyFilepath
			originalDynamicPath = DynamicTopologyFilepath

			MetadataTopologyFilepath = filepath.Join(tempDir, "metadata", "topo.json")
			DynamicTopologyFilepath = filepath.Join(tempDir, "dynamic", "topo.json")
		})

		AfterEach(func() {
			_ = os.RemoveAll(tempDir)
			MetadataTopologyFilepath = originalMetadataPath
			DynamicTopologyFilepath = originalDynamicPath
		})

		It("should do nothing if dynamic topology already exists", func() {
			err := os.MkdirAll(filepath.Dir(DynamicTopologyFilepath), 0755)
			Expect(err).NotTo(HaveOccurred())

			existingContent := []byte("existing content")
			err = os.WriteFile(DynamicTopologyFilepath, existingContent, 0644)
			Expect(err).NotTo(HaveOccurred())

			err = EnsureDynamicTopologyFiltered()
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(DynamicTopologyFilepath)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(existingContent))
		})

		It("should return error if no original topology exists", func() {
			err := EnsureDynamicTopologyFiltered()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no original topology file available"))
		})
	})
})

func createNewNamespace(ctx context.Context) string {
	namespace := "ns" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	ns := &corev1.Namespace{}
	ns.Name = namespace
	err := K8sClient.Create(ctx, ns)
	Expect(err).To(BeNil())
	return namespace
}

func createDevicesInTestDevicePath(deviceList []string) {
	for _, device := range deviceList {
		err := utils.CreateFolderIfNotExists(filepath.Join(devicePath, device))
		Expect(err).To(BeNil())
	}
}
