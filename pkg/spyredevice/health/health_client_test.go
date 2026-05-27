/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources"
	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/health"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/types"
)

type DummyServer struct {
	pb.UnimplementedSpyreHealthServiceServer

	grpcServer  *grpc.Server
	quit        chan struct{}
	SocketPath  string
	deviceQueue chan *pb.Devices
}

type SimplifiedDevice struct {
	PciAddress string
	State      pb.DEVICE_STATE
}

func (d SimplifiedDevice) Device() *pb.Device {
	return &pb.Device{
		DeviceID: &pb.DeviceID{
			PCIAddress: d.PciAddress,
		},
		DeviceType:  pb.DEVICE_TYPE_PF,
		DeviceState: d.State,
	}
}

func generateTestSock() string {
	f, err := os.CreateTemp("", "unixsock-*")
	Expect(err).To(BeNil())
	socketName := f.Name()
	_ = f.Close()
	_ = SafeRemove(socketName) // remove file so socket can be created
	return socketName
}

// loadServerTLSCredentials loads TLS credentials for the dummy server
func loadServerTLSCredentials() (credentials.TransportCredentials, error) {
	certPath := os.Getenv(TLSCertPathEnvKey)
	if certPath == "" {
		return nil, fmt.Errorf("TLS cert path not set")
	}

	keyPath := os.Getenv(TLSKeyPathEnvKey)
	if keyPath == "" {
		return nil, fmt.Errorf("TLS key path not set")
	}

	serverCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate and key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert, // pragma: allowlist secret
		MinVersion:   tls.VersionTLS12,
	}

	return credentials.NewTLS(tlsConfig), nil
}

func NewDummyServer() *DummyServer {
	return NewDummyServerWithSocket("")
}

func NewDummyServerWithSocket(socketPath string) *DummyServer {
	if socketPath == "" {
		socketPath = generateTestSock()
	}
	_ = os.Setenv(SpyreHealthSocketEnvKey, socketPath)
	By("Starting dummy server")

	tlsCreds, err := loadServerTLSCredentials()
	Expect(err).NotTo(HaveOccurred())

	grpcServer := grpc.NewServer(grpc.Creds(tlsCreds))
	dummyServer := &DummyServer{
		deviceQueue: make(chan *pb.Devices),
		quit:        make(chan struct{}),
		grpcServer:  grpcServer,
		SocketPath:  socketPath,
	}
	pb.RegisterSpyreHealthServiceServer(grpcServer, dummyServer)
	go func() {
		lis, err := net.Listen("unix", socketPath)
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("Listening at %s", socketPath))
		err = grpcServer.Serve(lis)
		Expect(err).NotTo(HaveOccurred())
	}()
	Eventually(func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	}, 5*time.Second, 500*time.Millisecond).Should(BeTrue(), "expected file to exist within timeout")
	return dummyServer
}

func (s *DummyServer) Stop() {
	close(s.quit)
	s.grpcServer.Stop()
	close(s.deviceQueue)
	_ = os.Unsetenv(SpyreHealthSocketEnvKey)
	_ = SafeRemove(s.SocketPath)
}

func (s *DummyServer) RegisterForSpyreDevicesEvents(in *emptypb.Empty,
	stream pb.SpyreHealthService_RegisterForSpyreDevicesEventsServer) error {
	_ = stream.Send(&pb.Devices{})
	for {
		select {
		case <-s.quit:
			return nil
		case devices := <-s.deviceQueue:
			_ = stream.Send(devices)
		}
	}
}

func (s *DummyServer) RegisterForSpyreDevicesEventsWithDevices(in *pb.Devices,
	stream pb.SpyreHealthService_RegisterForSpyreDevicesEventsServer) error {
	_ = stream.Send(&pb.Devices{})
	for {
		select {
		case <-s.quit:
			return nil
		case devices := <-s.deviceQueue:
			_ = stream.Send(devices)
		}
	}
}

func (s *DummyServer) UpdateDeviceState(devs []SimplifiedDevice) {
	devices := make([]*pb.Device, 0, len(devs))
	for _, dev := range devs {
		devices = append(devices, dev.Device())
	}
	s.deviceQueue <- &pb.Devices{
		Devices: devices,
	}
}

// createDummyTLSCertificates generates temporary self-signed certificates for testing
func createDummyTLSCertificates() error {
	certDir := "/tmp/certs"

	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("cannot create cert directory: %w", err)
	}

	certPath := filepath.Join(certDir, "tls.crt")
	keyPath := filepath.Join(certDir, "tls.key")

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}
	defer func() { _ = certOut.Close() }()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write cert: %w", err)
	}

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer func() { _ = keyOut.Close() }()
	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	return nil
}

func cleanupDummyTLSCertificates() {
	_ = os.RemoveAll("/tmp/certs")
}

var _ = Describe("SpyreHealthClient", Serial, Ordered, func() {
	BeforeAll(func() {
		_ = os.Setenv(TLSCertPathEnvKey, "/tmp/certs/tls.crt")
		_ = os.Setenv(TLSKeyPathEnvKey, "/tmp/certs/tls.key")

		if err := createDummyTLSCertificates(); err != nil {
			Skip(fmt.Sprintf("Cannot create TLS certificates for testing: %v", err))
		}
	})

	AfterAll(func() {
		cleanupDummyTLSCertificates()
		_ = os.Unsetenv(TLSCertPathEnvKey)
		_ = os.Unsetenv(TLSKeyPathEnvKey)
	})

	DescribeTable("MapsEqual", func(a, b map[string]any, expected bool) {
		Expect(MapsEqual(a, b)).To(Equal(expected))
	},
		Entry("empty", map[string]any{}, map[string]any{}, true),
		Entry("nil", nil, nil, true),
		Entry("equal", map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": healthyDeviceState,
		}, map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": healthyDeviceState,
		}, true),
		Entry("become unhealthy", map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": healthyDeviceState,
		}, map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": unhealthyDeviceState,
		}, false),
		Entry("different set", map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": healthyDeviceState,
		}, map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0003:00:00.0": healthyDeviceState,
		}, false),
		Entry("missing", map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": healthyDeviceState,
		}, map[string]any{
			"0001:00:00.0": healthyDeviceState,
		}, false),
		Entry("addition", map[string]any{
			"0001:00:00.0": healthyDeviceState,
		}, map[string]any{
			"0001:00:00.0": healthyDeviceState,
			"0002:00:00.0": healthyDeviceState,
		}, false),
		Entry("addition from empty", map[string]any{},
			map[string]any{
				"0001:00:00.0": healthyDeviceState,
			}, false),
		Entry("missing and become empty",
			map[string]any{
				"0001:00:00.0": healthyDeviceState,
			}, map[string]any{}, false),
	)

	Describe("spyreHealthSocket", func() {
		var originalEnvValue string
		var hasOriginalEnv bool

		BeforeEach(func() {
			originalEnvValue, hasOriginalEnv = os.LookupEnv(SpyreHealthSocketEnvKey)
		})

		AfterEach(func() {
			if hasOriginalEnv {
				_ = os.Setenv(SpyreHealthSocketEnvKey, originalEnvValue)
			} else {
				_ = os.Unsetenv(SpyreHealthSocketEnvKey)
			}
		})

		It("should return error when environment variable is unset", func() {
			_ = os.Unsetenv(SpyreHealthSocketEnvKey)
			_, err := SpyreHealthSocket()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is unset"))
		})

		It("should return error when socket file does not exist", func() {
			nonExistentSocket := "/tmp/nonexistent-socket-" + time.Now().Format("20060102150405")
			_ = os.Setenv(SpyreHealthSocketEnvKey, nonExistentSocket)
			_, err := SpyreHealthSocket()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unix socket is unavailable"))
		})

		It("should return error when path exists but is not a socket", func() {
			tmpFile, err := os.CreateTemp("", "not-a-socket-*")
			Expect(err).NotTo(HaveOccurred())
			tmpPath := tmpFile.Name()
			_ = tmpFile.Close()
			defer func() { _ = SafeRemove(tmpPath) }()

			Expect(os.Setenv(SpyreHealthSocketEnvKey, tmpPath)).To(Succeed())
			_, err = SpyreHealthSocket()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is not a unix socket"))
		})

		It("should prefix with 'unix://' when path contains '/'", func() {
			// Create a temporary socket
			socketPath := generateTestSock()
			lis, err := net.Listen("unix", socketPath)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = lis.Close() }()
			defer func() { _ = SafeRemove(socketPath) }()

			_ = os.Setenv(SpyreHealthSocketEnvKey, socketPath)
			result, err := SpyreHealthSocket()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("unix://" + socketPath))
		})

		It("should prefix with 'unix:' when path does not contain '/'", func() {
			socketPath := generateTestSock()
			lis, err := net.Listen("unix", socketPath)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = lis.Close() }()
			defer func() { _ = SafeRemove(socketPath) }()

			socketDir := filepath.Dir(socketPath)
			socketName := filepath.Base(socketPath)
			originalDir, err := os.Getwd()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Chdir(originalDir) }()

			err = os.Chdir(socketDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.Setenv(SpyreHealthSocketEnvKey, socketName)).To(Succeed())
			result, err := SpyreHealthSocket()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("unix:" + socketName))
		})
	})

	Describe("loadTLSCredentials", func() {
		var originalCertPath, originalKeyPath string
		var hasCertPath, hasKeyPath bool

		BeforeEach(func() {
			originalCertPath, hasCertPath = os.LookupEnv(TLSCertPathEnvKey)
			originalKeyPath, hasKeyPath = os.LookupEnv(TLSKeyPathEnvKey)
		})

		AfterEach(func() {
			if hasCertPath {
				_ = os.Setenv(TLSCertPathEnvKey, originalCertPath)
			} else {
				_ = os.Unsetenv(TLSCertPathEnvKey)
			}
			if hasKeyPath {
				_ = os.Setenv(TLSKeyPathEnvKey, originalKeyPath)
			} else {
				_ = os.Unsetenv(TLSKeyPathEnvKey)
			}
		})

		It("should load TLS credentials from environment variable paths", func() {
			creds, err := LoadTLSCredentials()
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).NotTo(BeNil())
		})

		It("should return error when certificate file does not exist", func() {
			Expect(os.Setenv(TLSCertPathEnvKey, "/tmp/nonexistent-cert.crt")).To(Succeed())
			Expect(os.Setenv(TLSKeyPathEnvKey, "/tmp/nonexistent-key.key")).To(Succeed())

			_, err := LoadTLSCredentials()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load client certificate and key"))
		})

		It("should return error when certificate file is invalid", func() {
			tmpDir, err := os.MkdirTemp("", "invalid-certs-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(tmpDir) }()

			invalidCertPath := filepath.Join(tmpDir, "invalid.crt")
			invalidKeyPath := filepath.Join(tmpDir, "invalid.key")
			err = os.WriteFile(invalidCertPath, []byte("invalid certificate"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(invalidKeyPath, []byte("invalid key"), 0644)
			Expect(err).NotTo(HaveOccurred())

			Expect(os.Setenv(TLSCertPathEnvKey, invalidCertPath)).To(Succeed())
			Expect(os.Setenv(TLSKeyPathEnvKey, invalidKeyPath)).To(Succeed())

			_, err = LoadTLSCredentials()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load client certificate and key"))
		})

		It("should use default paths when environment variables are not set", func() {
			Expect(os.Unsetenv(TLSCertPathEnvKey)).To(Succeed())
			Expect(os.Unsetenv(TLSKeyPathEnvKey)).To(Succeed())

			_, err := LoadTLSCredentials()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load client certificate and key"))
		})

		It("should create TLS config with correct settings", func() {
			creds, err := LoadTLSCredentials()
			Expect(err).NotTo(HaveOccurred())
			Expect(creds).NotTo(BeNil())

			info := creds.Info()
			Expect(info.SecurityProtocol).To(Equal("tls"))
		})
	})

	Context("TLS Configuration", func() {
		It("should require TLS certificates and fail without them", func() {
			cleanupDummyTLSCertificates()
			defer func() {
				Expect(createDummyTLSCertificates()).To(Succeed())
			}()

			// Try to load TLS credentials without certificates
			_, err := LoadTLSCredentials()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load"))
		})
	})

	Context("Lifecycle", Ordered, func() {
		var dummyServer *DummyServer
		var client *SpyreHealthClient
		var updateChan chan struct{}
		var ctx context.Context
		var cancel context.CancelFunc
		BeforeAll(func() {
			ctx, cancel = context.WithCancel(context.Background())
			var err error
			dummyServer = NewDummyServer()
			client, err = NewSpyreHealthClient()
			Expect(err).NotTo(HaveOccurred())
			updateChan = make(chan struct{})
			err = client.Start(ctx, updateChan, &pb.Devices{})
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Running()).To(BeTrue())
		})

		DescribeTable("listen and setAndNotify", Ordered, func(
			input []SimplifiedDevice,
			presentHealthInfoMap map[string]any,
			allDevices []types.PciDevice,
			expectedUpdates map[string]DeviceHealthState) {
			dummyServer.UpdateDeviceState(input)
			client.SetHealths(presentHealthInfoMap)
			if expectedUpdates != nil {
				_, ok := <-updateChan
				Expect(ok).To(BeTrue())
				healthInfoMap := InitHealthInfo(allDevices)
				client.UpdateHealths(healthInfoMap)
				Expect(healthInfoMap).To(HaveLen(len(expectedUpdates)))
				for k, v := range healthInfoMap {
					expected := expectedUpdates[k]
					Expect(v).To(Equal(expected))
				}
			} else {
				Expect(updateChan).To(HaveLen(0))
			}
		},
			Entry("no device", []SimplifiedDevice{}, map[string]any{}, nil, nil),
			Entry("no new device info",
				[]SimplifiedDevice{{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_ONLINE}},
				map[string]any{
					"0001:00:00.0": healthyDeviceState,
				},
				nil, nil),
			Entry("new one healthy device",
				[]SimplifiedDevice{{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_ONLINE}},
				map[string]any{},
				[]types.PciDevice{createTestPciDevice("0001:00:00.0", resources.PfProductId)},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
				}),
			Entry("update unhealthy device",
				[]SimplifiedDevice{{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_IN_ERROR}},
				map[string]any{
					"0001:00:00.0": healthyDeviceState,
				},
				[]types.PciDevice{createTestPciDevice("0001:00:00.0", resources.PfProductId)},
				map[string]DeviceHealthState{
					"0001:00:00.0": unhealthyDeviceState,
				}),
			Entry("one healthy device, one unhealthy device",
				[]SimplifiedDevice{
					{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_ONLINE},
					{PciAddress: "0002:00:00.0", State: pb.DEVICE_STATE_IN_ERROR},
				},
				map[string]any{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
				},
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0002:00:00.0", resources.PfProductId),
				},
				map[string]DeviceHealthState{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": unhealthyDeviceState,
				}),
			Entry("one device removed, added, and status change at the same time",
				[]SimplifiedDevice{
					{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_IN_ERROR},
					{PciAddress: "0003:00:00.0", State: pb.DEVICE_STATE_ONLINE},
				},
				map[string]any{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
					"0003:00:00.0": healthyDeviceState,
				},
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					createTestPciDevice("0003:00:00.0", resources.PfProductId),
				},
				map[string]DeviceHealthState{
					"0001:00:00.0": unhealthyDeviceState,
					"0003:00:00.0": healthyDeviceState,
				}),
			Entry("update unsync, informed device missing from present list",
				[]SimplifiedDevice{
					{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_IN_ERROR},
					{PciAddress: "0002:00:00.0", State: pb.DEVICE_STATE_IN_ERROR},
				},
				map[string]any{
					"0001:00:00.0": healthyDeviceState,
					"0002:00:00.0": healthyDeviceState,
				},
				[]types.PciDevice{
					createTestPciDevice("0001:00:00.0", resources.PfProductId),
					// "0002:00:00.0" is removed
				},
				map[string]DeviceHealthState{
					// still, "0001:00:00.0" must be updated
					"0001:00:00.0": unhealthyDeviceState,
				}),
		)

		AfterAll(func() {
			if client.Running() {
				By("Stopping client")
				client.Stop()
			}
			dummyServer.Stop()
			cancel()
			By("Ensuring client stop")
			Eventually(func(g Gomega) {
				g.Expect(client.Running()).To(BeFalse())
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
		})

	})

	Context("Reconnection", Ordered, func() {
		var dummyServer *DummyServer
		var client *SpyreHealthClient
		var updateChan chan struct{}
		var ctx context.Context
		var cancel context.CancelFunc

		BeforeAll(func() {
			ctx, cancel = context.WithCancel(context.Background())
		})

		AfterAll(func() {
			cancel()
		})

		It("should reconnect after server restart", func() {
			// Start initial server
			dummyServer = NewDummyServer()
			var err error
			client, err = NewSpyreHealthClient()
			Expect(err).NotTo(HaveOccurred())

			// Configure for faster reconnection in tests
			client.SetMaxReconnectAttempts(5)
			client.SetInitialBackoff(100 * time.Millisecond)
			client.SetMaxBackoff(1 * time.Second)

			updateChan = make(chan struct{}, 10)
			err = client.Start(ctx, updateChan, &pb.Devices{})
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Running()).To(BeTrue())

			// Send initial device state
			dummyServer.UpdateDeviceState([]SimplifiedDevice{
				{PciAddress: "0001:00:00.0", State: pb.DEVICE_STATE_ONLINE},
			})

			// Wait for initial update
			Eventually(updateChan, 5*time.Second).Should(Receive())

			// Stop the server to simulate disconnection
			By("Stopping server to simulate disconnection")
			dummyServer.Stop()

			// Wait for client to detect disconnection
			Eventually(func() bool {
				return !client.Running()
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Restart server with same socket path
			By("Restarting server")
			socketPath := dummyServer.SocketPath
			dummyServer = NewDummyServerWithSocket(socketPath)

			// Client should automatically reconnect
			By("Waiting for client to reconnect")
			Eventually(func() bool {
				return client.Running()
			}, 10*time.Second, 200*time.Millisecond).Should(BeTrue())

			// Verify client can receive updates after reconnection
			By("Sending device state after reconnection")
			dummyServer.UpdateDeviceState([]SimplifiedDevice{
				{PciAddress: "0002:00:00.0", State: pb.DEVICE_STATE_ONLINE},
			})

			// Should receive update
			Eventually(updateChan, 5*time.Second).Should(Receive())

			// Cleanup
			client.Stop()
			dummyServer.Stop()
		})

		It("should stop reconnection attempts when quit signal is received", func() {
			// Start server
			dummyServer = NewDummyServer()
			var err error
			client, err = NewSpyreHealthClient()
			Expect(err).NotTo(HaveOccurred())

			// Configure for slower reconnection to test quit
			client.SetMaxReconnectAttempts(10)
			client.SetInitialBackoff(500 * time.Millisecond)

			updateChan = make(chan struct{}, 10)
			err = client.Start(ctx, updateChan, &pb.Devices{})
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Running()).To(BeTrue())

			// Stop server to trigger reconnection
			By("Stopping server to trigger reconnection")
			dummyServer.Stop()

			// Wait for disconnection
			Eventually(func() bool {
				return !client.Running()
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Stop client during reconnection attempts
			By("Stopping client during reconnection")
			client.Stop()

			// Verify client stays stopped and doesn't reconnect
			Consistently(func() bool {
				return !client.Running()
			}, 3*time.Second, 200*time.Millisecond).Should(BeTrue())
		})

		It("should respect max reconnection attempts", func() {
			// Start server then stop it immediately to test reconnection failure
			dummyServer = NewDummyServer()
			var err error
			client, err = NewSpyreHealthClient()
			Expect(err).NotTo(HaveOccurred())

			// Configure for fast failure with limited attempts
			client.SetMaxReconnectAttempts(3)
			client.SetInitialBackoff(100 * time.Millisecond)
			client.SetMaxBackoff(500 * time.Millisecond)

			updateChan = make(chan struct{}, 10)
			err = client.Start(ctx, updateChan, &pb.Devices{})
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Running()).To(BeTrue())

			// Stop and remove the server/socket to prevent reconnection
			By("Stopping server and removing socket")
			socketPath := dummyServer.SocketPath
			dummyServer.Stop()
			_ = SafeRemove(socketPath)

			// Wait for client to detect disconnection and fail to reconnect
			Eventually(func() bool {
				return !client.Running()
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())

			// After max attempts, client should remain stopped
			Consistently(func() bool {
				return !client.Running()
			}, 2*time.Second, 200*time.Millisecond).Should(BeTrue())

			// Cleanup
			client.Stop()
		})
	})

})
