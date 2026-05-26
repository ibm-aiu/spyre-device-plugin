/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package manager_test

import (
	"os"
	"path/filepath"
	"testing"

	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Manager Suite")
}

var (
	testHostPath        = "./test"
	testMetricsHostPath = filepath.Join(testHostPath, spyreconf.SpyreMetricBaseFolderName)
	testEnv             *envtest.Environment
	testCfg             *rest.Config
)

var _ = BeforeSuite(func() {
	utils.CreateFolderIfNotExists(testHostPath)
	os.Setenv(spyreconf.MetricsHostPathKey, testMetricsHostPath)

	// Set up envtest for controller-runtime manager
	testEnv = &envtest.Environment{}
	var err error
	testCfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(testCfg).NotTo(BeNil())

	// Set the test config as the default for controller-runtime
	// This allows ctrl.GetConfigOrDie() to work in tests
	os.Setenv("KUBECONFIG", testCfg.Host)
})

var _ = AfterSuite(func() {
	os.Unsetenv(spyreconf.MetricsHostPathKey)
	os.Unsetenv("KUBECONFIG")
	os.RemoveAll(testHostPath)

	// Stop the test environment
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
