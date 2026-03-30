/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	pb "github.com/ibm-aiu/spyre-health-checker/pkg/health/spyre"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// TODO: move to spyre operator const
	SpyreHealthSocketEnvKey = "SPYRE_HEALTH_SOCK"

	// TLS configuration
	TLSCertPathEnvKey  = "SPYRE_HEALTH_TLS_CERT_PATH"
	TLSKeyPathEnvKey   = "SPYRE_HEALTH_TLS_KEY_PATH"
	DefaultTLSCertPath = "/etc/device-plugin/certs/tls.crt"
	DefaultTLSKeyPath  = "/etc/device-plugin/certs/tls.key"

	// Reconnection configuration
	DefaultMaxReconnectAttempts = 10
	DefaultInitialBackoff       = 1 * time.Second
	DefaultMaxBackoff           = 60 * time.Second
	DefaultBackoffMultiplier    = 2.0
)

func spyreHealthSocket() (string, error) {
	socketName := os.Getenv(SpyreHealthSocketEnvKey)
	if socketName == "" {
		return "", fmt.Errorf("%s is unset", SpyreHealthSocketEnvKey)
	}
	info, err := os.Stat(socketName)
	if err != nil {
		return socketName, fmt.Errorf("unix socket is unavailable: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return socketName, fmt.Errorf("%s is not a unix socket", socketName)
	}
	if strings.Contains(socketName, "/") {
		socketName = "unix://" + socketName
	} else {
		socketName = "unix:" + socketName
	}
	return socketName, nil
}

func loadTLSCredentials() (credentials.TransportCredentials, error) {
	certPath := os.Getenv(TLSCertPathEnvKey)
	if certPath == "" {
		certPath = DefaultTLSCertPath
	}

	keyPath := os.Getenv(TLSKeyPathEnvKey)
	if keyPath == "" {
		keyPath = DefaultTLSKeyPath
	}

	glog.Infof("Loading TLS credentials")

	clientCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate and key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
	}

	glog.Info("TLS credentials loaded successfully for spyre health client")
	return credentials.NewTLS(tlsConfig), nil
}

type SpyreHealthClient struct {
	socketName    string
	healthInfoMap map[string]any
	mu            sync.RWMutex // Protects healthInfoMap and conn
	conn          *grpc.ClientConn
	insecure      bool // If true, connect without TLS

	running              atomic.Bool
	quit                 chan interface{}
	reconnecting         atomic.Bool
	maxReconnectAttempts int
	initialBackoff       time.Duration
	maxBackoff           time.Duration
	backoffMultiplier    float64
}

func NewSpyreHealthClient(insecure bool) (*SpyreHealthClient, error) {
	socketName, err := spyreHealthSocket()
	if err != nil {
		return nil, fmt.Errorf("failed to get spyrehealth socket: %w", err)
	}
	return &SpyreHealthClient{
		socketName:           socketName,
		healthInfoMap:        make(map[string]any),
		insecure:             insecure,
		quit:                 make(chan interface{}),
		maxReconnectAttempts: DefaultMaxReconnectAttempts,
		initialBackoff:       DefaultInitialBackoff,
		maxBackoff:           DefaultMaxBackoff,
		backoffMultiplier:    DefaultBackoffMultiplier,
	}, nil
}

func (t *SpyreHealthClient) Running() bool {
	return t.running.Load()
}

// SetMaxReconnectAttempts sets the maximum number of reconnection attempts
func (t *SpyreHealthClient) SetMaxReconnectAttempts(attempts int) {
	t.maxReconnectAttempts = attempts
}

// SetInitialBackoff sets the initial backoff duration for reconnection
func (t *SpyreHealthClient) SetInitialBackoff(duration time.Duration) {
	t.initialBackoff = duration
}

// SetMaxBackoff sets the maximum backoff duration for reconnection
func (t *SpyreHealthClient) SetMaxBackoff(duration time.Duration) {
	t.maxBackoff = duration
}

// SetBackoffMultiplier sets the backoff multiplier for exponential backoff
func (t *SpyreHealthClient) SetBackoffMultiplier(multiplier float64) {
	t.backoffMultiplier = multiplier
}

func (t *SpyreHealthClient) Start(ctx context.Context, updateChan chan struct{}, initialDevices *pb.Devices) error {
	if err := t.Register(ctx, updateChan, initialDevices); err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}
	return nil
}

func (t *SpyreHealthClient) Stop() {
	close(t.quit)
	glog.Info("SpyreHealthClient stopped")
}

func (t *SpyreHealthClient) UpdateHealths(healthInfoMap map[string]DeviceHealthState) {
	for deviceID, healthInfo := range t.healthInfoMap {
		if _, found := healthInfoMap[deviceID]; found { // update only found item
			healthInfoMap[deviceID] = healthInfo.(DeviceHealthState)
		}
	}
}

// Register setups and receive stream
func (t *SpyreHealthClient) Register(ctx context.Context, updateChan chan struct{}, initialDevices *pb.Devices) error {
	if t.Running() {
		return nil
	}
	var err error

	var dialOpts []grpc.DialOption

	if t.insecure {
		glog.Warning("Using insecure connection for spyre health client (TLS disabled)")
		dialOpts = []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
	} else {
		creds, err := loadTLSCredentials() // pragma: allowlist secret
		if err != nil {
			return fmt.Errorf("failed to load TLS credentials: %w", err) // pragma: allowlist secret
		}
		glog.Info("Using TLS for spyre health client connection")
		dialOpts = []grpc.DialOption{
			grpc.WithTransportCredentials(creds),
		}
	}

	t.mu.Lock()
	t.conn, err = grpc.NewClient(t.socketName, dialOpts...)
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("unable to establish connection with SpyreHealth gRPC server %s: %v", t.socketName, err)
	}
	client := pb.NewSpyreHealthServiceClient(t.conn)
	t.mu.Unlock()

	stream, err := client.RegisterForSpyreDevicesEventsWithDevices(ctx, initialDevices)
	if err != nil {
		t.mu.Lock()
		_ = t.conn.Close()
		t.mu.Unlock()
		return fmt.Errorf("error calling stream: %v", err)
	}
	t.running.Store(true)
	glog.Info("Health client registered")
	go t.listen(ctx, stream, updateChan, initialDevices) //nolint:errcheck
	return nil
}

func (t *SpyreHealthClient) listen(ctx context.Context,
	stream pb.SpyreHealthService_RegisterForSpyreDevicesEventsClient, updateChan chan struct{}, devices *pb.Devices) error {
	defer func() {
		t.running.Store(false)
		t.mu.Lock()
		if t.conn != nil {
			_ = t.conn.Close()
		}
		t.mu.Unlock()
		if r := recover(); r != nil {
			glog.Info("Recovered from panic (update channel probably closed)")
		}
	}()
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			glog.Info("SpyreHealthClient connection closed (EOF), attempting reconnection")
			go t.attemptReconnect(ctx, updateChan, devices)
			return nil
		}
		select {
		case <-t.quit:
			glog.Info("Quit signal, stop tracking devices")
			return nil
		case <-ctx.Done():
			return nil
		default:
			if err != nil {
				glog.Warningf("Unexpected stream error: %v, attempting reconnection", err)
				go t.attemptReconnect(ctx, updateChan, devices)
				return fmt.Errorf("stream error : %v", err)
			}
			go t.setAndNotify(res.Devices, updateChan)
		}
	}
}

// setAndNotify updates devices info,
// and notify if change
func (t *SpyreHealthClient) setAndNotify(devices []*pb.Device, updateChan chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	glog.V(1).Info("Set latest value and notify handler if changes")
	currentDeviceMap := make(map[string]any, len(devices))
	for _, device := range devices {
		deviceID := device.DeviceID.PCIAddress
		healthInfo := DeviceHealthState{
			state:      device.DeviceState,
			deviceType: device.DeviceType,
		}
		currentDeviceMap[deviceID] = healthInfo
	}

	if !MapsEqual(t.healthInfoMap, currentDeviceMap) {
		for addr := range t.healthInfoMap {
			if _, exists := currentDeviceMap[addr]; !exists {
				glog.Infof("Device %s removed", addr)
			}
		}
		for addr, curInfo := range currentDeviceMap {
			if newInfo, exists := t.healthInfoMap[addr]; !exists {
				glog.Infof("Device %s added", addr)
			} else if curInfo.(DeviceHealthState).state != newInfo.(DeviceHealthState).state {
				glog.Infof("Device % s health info changed", addr)
			}
		}
		t.healthInfoMap = currentDeviceMap
		SafeTriggerUpdate(updateChan)
	}
}

// attemptReconnect tries to reconnect to the spyrehealth socket with exponential backoff
func (t *SpyreHealthClient) attemptReconnect(ctx context.Context, updateChan chan struct{}, devices *pb.Devices) {
	// Prevent multiple concurrent reconnection attempts
	if !t.reconnecting.CompareAndSwap(false, true) {
		glog.V(1).Info("Reconnection already in progress, skipping")
		return
	}
	defer t.reconnecting.Store(false)

	backoff := t.initialBackoff
	for attempt := 1; attempt <= t.maxReconnectAttempts; attempt++ {
		select {
		case <-t.quit:
			glog.Info("Quit signal received during reconnection, stopping")
			return
		case <-ctx.Done():
			glog.Info("Context cancelled during reconnection, stopping")
			return
		default:
		}

		glog.Infof("Reconnection attempt %d/%d to spyrehealth socket", attempt, t.maxReconnectAttempts)

		// Wait before attempting reconnection (except for first attempt)
		if attempt > 1 {
			select {
			case <-time.After(backoff):
			case <-t.quit:
				glog.Info("Quit signal received during backoff, stopping reconnection")
				return
			case <-ctx.Done():
				glog.Info("Context cancelled during backoff, stopping reconnection")
				return
			}
		}

		// Attempt to reconnect
		err := t.Register(ctx, updateChan, devices)
		if err == nil {
			glog.Infof("Successfully reconnected to spyrehealth socket on attempt %d", attempt)
			return
		}

		glog.Warningf("Reconnection attempt %d failed: %v", attempt, err)

		// Calculate next backoff with exponential increase
		backoff = time.Duration(float64(backoff) * t.backoffMultiplier)
		if backoff > t.maxBackoff {
			backoff = t.maxBackoff
		}
	}

	glog.Errorf("Failed to reconnect to spyrehealth socket after %d attempts", t.maxReconnectAttempts)
}
