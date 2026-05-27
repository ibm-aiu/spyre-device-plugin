package dma

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDMA(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "P2P DMA Suite")
}

var _ = Describe("DMA", func() {

	Context("with temporary SysBusPci", Ordered, func() {
		var originalSysBusPci string
		tmpSysBusPciPath := "./tmp-sys-bus-pci"
		expectedDevice := "00:00.0"
		unexpectedDevice := "00:0a.0"

		devResourceFileName := filepath.Join(tmpSysBusPciPath, expectedDevice, "resource2")

		BeforeAll(func() {
			originalSysBusPci = utils.SysBusPci
			utils.SysBusPci = tmpSysBusPciPath
			err := utils.CreateFolderIfNotExists(tmpSysBusPciPath)
			Expect(err).NotTo(HaveOccurred())
			err = utils.CreateFolderIfNotExists(filepath.Join(tmpSysBusPciPath, expectedDevice))
			Expect(err).NotTo(HaveOccurred())

		})

		AfterAll(func() {
			utils.SysBusPci = originalSysBusPci
			err := os.RemoveAll(tmpSysBusPciPath)
			Expect(err).NotTo(HaveOccurred())
		})

		BeforeEach(func() {
			// create resource2 file
			file, err := os.OpenFile(devResourceFileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			Expect(err).To(BeNil())
			defer func() { _ = file.Close() }()
		})

		AfterEach(func() {
			err := os.Remove(devResourceFileName)
			Expect(err).To(BeNil())
		})

		DescribeTable("Set-UnsetDevResourceFilePermissions",
			func(goodDevices []string, requestDevices []string, errorExpected bool) {
				By("Setting good devices")
				for _, dev := range goodDevices {
					devPath := filepath.Join(tmpSysBusPciPath, dev)
					err := utils.CreateFolderIfNotExists(devPath)
					Expect(err).NotTo(HaveOccurred())
					defer func(path string) {
						_ = os.RemoveAll(path)
					}(devPath)
					file, err := os.OpenFile(filepath.Join(devPath, "resource2"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
					Expect(err).To(BeNil())
					defer func() { _ = file.Close() }()
				}
				By("Testing SetDevResourceFilePermissions")
				err := SetDevResourceFilePermissions(requestDevices)
				if errorExpected {
					By("Testing undo")
					for _, dev := range requestDevices {
						if slices.Contains(goodDevices, dev) {
							devPath := filepath.Join(tmpSysBusPciPath, dev)
							resourceFile := filepath.Join(devPath, "resource2")
							checkFilePermission(resourceFile, 0600)
						}
					}
					return
				}
				Expect(err).NotTo(HaveOccurred())
				for _, dev := range requestDevices {
					devPath := filepath.Join(tmpSysBusPciPath, dev)
					resourceFile := filepath.Join(devPath, "resource2")
					checkFilePermission(resourceFile, 0666)
				}
				UnsetDevResourceFilePermissions(requestDevices)
				for _, dev := range requestDevices {
					devPath := filepath.Join(tmpSysBusPciPath, dev)
					resourceFile := filepath.Join(devPath, "resource2")
					checkFilePermission(resourceFile, 0600)
				}
			},
			Entry("single good device", []string{"10:00.0"}, []string{"10:00.0"}, false),
			Entry("multiple good devices", []string{"10:00.0", "11:00.0"}, []string{"10:00.0", "11:00.0"}, false),
			Entry("mixed multiple devices - tail failed", []string{"10:00.0", "11:00.0"}, []string{"10:00.0", "12:00.0"}, true),
			Entry("mixed multiple devices - head failed", []string{"10:00.0", "11:00.0"}, []string{"12:00.0", "11:00.0"}, true),
		)

		It("set missing devices", func() {
			err := SetDevResourceFilePermissions([]string{unexpectedDevice})
			Expect(err.Error()).To(ContainSubstring("not exists"))
		})

		It("unset missing devices", func() {
			errs := UnsetDevResourceFilePermissions([]string{unexpectedDevice})
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Error()).To(ContainSubstring("not exists"))
		})
	})

	Context("check P2PDMA requirement", func() {
		multipleDevices := []string{"dev01", "dev02"}
		DescribeTable("NeedP2PDMAConfigure", func(deviceList []string, p2pDMA, pseudoMode, expected bool) {
			if pseudoMode {
				_ = os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), spyreconst.ModeEnabledValue)
				defer func() { _ = os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey()) }()
			}
			if p2pDMA {
				Expect(os.Setenv(P2PDMAEnvKey, spyreconst.ModeEnabledValue)).To(Succeed())
				defer func() { Expect(os.Unsetenv(P2PDMAEnvKey)).To(Succeed()) }()
			}
			support := NeedP2PDMAConfigure(deviceList)
			Expect(support).To(Equal(expected))
		},
			Entry("p2pDMA=0", multipleDevices, false, false, false),
			Entry("p2pDMA=0 with pseudoMode", multipleDevices, false, true, false),
			Entry("p2pDMA=1", multipleDevices, true, false, true),
			Entry("p2pDMA=1 no device", []string{}, true, false, false),
			Entry("p2pDMA=1 single device", []string{"dev01"}, true, false, false),
			Entry("p2pDMA=1 with pseudoMode", multipleDevices, true, true, false),
		)
	})

})

func checkFilePermission(filePath string, expected uint32) {
	info, err := os.Stat(filePath)
	Expect(err).NotTo(HaveOccurred())
	perm := info.Mode().Perm()
	Expect(perm).To(Equal(os.FileMode(expected)))
}
