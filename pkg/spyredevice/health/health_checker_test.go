/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health_test

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/health"
	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	spyreconst "github.com/ibm-aiu/spyre-operator/const"
)

var _ = Describe("HealthChecker", Serial, Ordered, func() {
	BeforeAll(func() {
		os.Setenv(TLSCertPathEnvKey, "/tmp/certs/tls.crt")
		os.Setenv(TLSKeyPathEnvKey, "/tmp/certs/tls.key")
		if err := createDummyTLSCertificates(); err != nil {
			Skip(fmt.Sprintf("Cannot create TLS certificates for testing: %v", err))
		}
	})

	AfterAll(func() {
		cleanupDummyTLSCertificates()
		os.Unsetenv(TLSCertPathEnvKey)
		os.Unsetenv(TLSKeyPathEnvKey)
	})

	Context("GetHealthChecker", func() {
		It("can return expected nil", func() {
			_, err := NewSpyreHealthClient(false)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(HavePrefix("failed to get spyrehealth socket"))
			os.Setenv(spyrev1alpha1.PseudoDeviceMode.EnvKey(), spyreconst.ModeEnabledValue)
			checker := GetHealthChecker(DefaultScanInterval, false)
			Expect(checker).To(BeNil(), "should not get %v", checker)
			os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
		})
		It("can get PciMonitor", func() {
			_, err := NewSpyreHealthClient(false)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(HavePrefix("failed to get spyrehealth socket"))
			os.Unsetenv(spyrev1alpha1.PseudoDeviceMode.EnvKey())
			customScanInterval := 99 * time.Second
			checker := GetHealthChecker(customScanInterval, false)
			pciMonitor, ok := checker.(*PCIMonitor)
			Expect(ok).To(BeTrue(), "should not get %v", pciMonitor)
			Expect(pciMonitor.ScanInterval).To(Equal(customScanInterval))
		})
		It("can get SpyreHealthClient", func() {
			dummyServer := NewDummyServer()
			defer dummyServer.Stop()
			checker := GetHealthChecker(DefaultScanInterval, false)
			client, ok := checker.(*SpyreHealthClient)
			Expect(ok).To(BeTrue(), "should not get %v", client)
		})
	})

})
