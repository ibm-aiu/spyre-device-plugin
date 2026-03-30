/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	spyrert "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/runtime"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	rtv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var Cfg *rest.Config
var SpyreClient *spyreclient.SpyreClient
var K8sClientset *kubernetes.Clientset
var testEnv *envtest.Environment
var lis net.Listener
var senlibConfigGnerator spyreconf.SenlibConfigGenerator
var ServerRunning bool

type ResourceServer = resourceServer

var CreateNewNamespace = createNewNamespace

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Server Suite")
}

var (
	socketFile          = "/tmp/fake-crio.sock"
	TestConfigHostPath  = "config-host-path"
	TestMetricsHostPath = "metrics-host-path"
	testSenlibTemplate  = "../../../test/data/senlib_config"
	fakeUnixSocketPath  = fmt.Sprintf("unix://%s", socketFile)

	NodeDeviceIds = []string{"00:01", "00:02"}
)

func startRuntimeServer() {
	os.Remove(socketFile)
	var err error
	// Create a listener on a fake Unix socket path
	lis, err = net.Listen("unix", socketFile)
	if err == nil {
		ServerRunning = true
		defer lis.Close()
		// Create gRPC server
		grpcServer := grpc.NewServer()
		// Register mock server to handle requests
		rtv1.RegisterRuntimeServiceServer(grpcServer, NewMockRuntimeServiceServer())
		log.Printf("Fake CRI-O server listening on %s\n", fakeUnixSocketPath)
		// Serve incoming requests
		err = grpcServer.Serve(lis)
	}
	if err != nil {
		log.Printf("failed to start runtime server %v", err)
	}
}

// var scheme = runtime.NewScheme()
func createNewNamespace() string {
	namespace := "ns" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	ns := &corev1.Namespace{}
	ns.Name = namespace
	_, err := K8sClientset.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	Expect(err).To(BeNil())
	return namespace
}

var _ = BeforeSuite(func() {
	os.Setenv(spyreconf.TemplatePathKey, testSenlibTemplate)
	os.Setenv(spyrert.RuntimeUnixSocketKey, fakeUnixSocketPath)
	go startRuntimeServer()

	var err error
	err = utils.CreateFolderIfNotExists(TestConfigHostPath)
	Expect(err).To(BeNil())
	err = utils.CreateFolderIfNotExists(TestMetricsHostPath)
	Expect(err).To(BeNil())
	senlibConfigGnerator = spyreconf.NewSenlibConfigGenerator()

	Expect(os.Getenv("KUBEBUILDER_ASSETS")).ShouldNot(Equal(""), "environment value \"KUBEBUILDER_ASSETS\" must be set.")

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
	Cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(Cfg).NotTo(BeNil())

	// create namespace "test"
	K8sClientset = kubernetes.NewForConfigOrDie(Cfg)
	Expect(err).To(BeNil())
	ns := &corev1.Namespace{}
	ns.Name = "test"
	_, err = K8sClientset.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	Expect(err).To(BeNil())

	SpyreClient, err = spyreclient.NewClient(context.Background(), Cfg)
	Expect(err).To(BeNil())
	Expect(SpyreClient).NotTo((BeNil()))

})

var _ = AfterSuite(func() {
	lis.Close()
	os.Remove(fakeUnixSocketPath)
	os.RemoveAll(TestConfigHostPath)
	os.RemoveAll(TestMetricsHostPath)

	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
