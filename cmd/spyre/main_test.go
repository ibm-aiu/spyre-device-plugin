/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package main

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

var _ = BeforeSuite(func() {

	var err error

	Expect(os.Getenv("KUBEBUILDER_ASSETS")).ShouldNot(Equal(""), "environment value \"KUBEBUILDER_ASSETS\" must be set.")

	By("bootstrapping test environment")
	crdPath := filepath.Join("..", "..", "config", "crd", "external")
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

	// create namespace "test"
	k8sClient, err = client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	ns := &corev1.Namespace{}
	ns.Name = "test"
	err = k8sClient.Create(context.Background(), ns)
	Expect(err).NotTo(HaveOccurred())

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
