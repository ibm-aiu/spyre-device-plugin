/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health_test

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var cfg *rest.Config
var testEnv *envtest.Environment

func TestSpyredeviceHealth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spyredevice Health Suite")
}

var _ = BeforeSuite(func() {
	var err error

	By("bootstrapping test environment")
	crdPath := filepath.Join("..", "..", "..", "config", "crd", "external")
	_, err = os.Stat(crdPath)
	Expect(err).To(
		BeNil(),
		"%v not exist; spyre-operator must exists the same directory of the device plugin code",
		crdPath)
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
