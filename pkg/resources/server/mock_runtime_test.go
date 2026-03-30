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
	"path/filepath"
	"sync"

	"github.com/golang/glog"
	"github.com/google/uuid"
	spyreconf "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	spyrert "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/runtime"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	. "k8s.io/cri-api/pkg/apis/runtime/v1"

	. "github.com/onsi/gomega"
)

const (
	uuidGenerateMaxRetry = 10
)

type MockRuntimeServiceServer struct {
	mu            sync.RWMutex
	fakeContainer map[string]CreateContainerRequest
}

func NewMockRuntimeServiceServer() *MockRuntimeServiceServer {
	return &MockRuntimeServiceServer{
		fakeContainer: make(map[string]CreateContainerRequest),
	}
}

func (s *MockRuntimeServiceServer) GenerateNewContainerId() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 1; attempt <= uuidGenerateMaxRetry; attempt++ {
		newUuid := uuid.NewString()
		if _, found := s.fakeContainer[newUuid]; !found {
			return newUuid
		}
	}
	return ""
}

func (s *MockRuntimeServiceServer) CreateContainer(ctx context.Context, req *CreateContainerRequest) (*CreateContainerResponse, error) {
	containerId := s.GenerateNewContainerId()
	if containerId == "" {
		return nil, fmt.Errorf("failed to generate new container ID (max retry: %d)", uuidGenerateMaxRetry)
	}
	s.mu.Lock()
	s.fakeContainer[containerId] = *req
	s.mu.Unlock()
	glog.Infof("successfully create container %v", *req)
	return &CreateContainerResponse{ContainerId: containerId}, nil
}

func (s *MockRuntimeServiceServer) ListContainers(ctx context.Context, req *ListContainersRequest) (*ListContainersResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	containers := make([]*Container, len(s.fakeContainer))
	for containerId := range s.fakeContainer {
		container := &Container{
			Id: containerId,
		}
		containers = append(containers, container)
	}
	glog.Infof("successfully list containers %v", containers)
	return &ListContainersResponse{
		Containers: containers,
	}, nil
}

func (s *MockRuntimeServiceServer) ContainerStatus(ctx context.Context, req *ContainerStatusRequest) (*ContainerStatusResponse, error) {
	s.mu.RLock()
	createRequest, found := s.fakeContainer[req.ContainerId]
	s.mu.RUnlock()
	if !found {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}
	containerConfig := createRequest.Config
	status := &ContainerStatus{
		Mounts: containerConfig.GetMounts(),
		State:  ContainerState_CONTAINER_RUNNING,
	}
	return &ContainerStatusResponse{
		Status: status,
	}, nil
}

func (s *MockRuntimeServiceServer) RemoveContainer(ctx context.Context, req *RemoveContainerRequest) (*RemoveContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.fakeContainer[req.ContainerId]; !found {
		return &RemoveContainerResponse{}, fmt.Errorf("not found")
	}
	delete(s.fakeContainer, req.ContainerId)
	return &RemoveContainerResponse{}, nil
}

// Keep unused call unimplemented
func (*MockRuntimeServiceServer) Version(ctx context.Context, req *VersionRequest) (*VersionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Version not implemented")
}
func (*MockRuntimeServiceServer) RunPodSandbox(ctx context.Context, req *RunPodSandboxRequest) (*RunPodSandboxResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RunPodSandbox not implemented")
}
func (*MockRuntimeServiceServer) StopPodSandbox(ctx context.Context, req *StopPodSandboxRequest) (*StopPodSandboxResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StopPodSandbox not implemented")
}
func (*MockRuntimeServiceServer) RemovePodSandbox(ctx context.Context, req *RemovePodSandboxRequest) (*RemovePodSandboxResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RemovePodSandbox not implemented")
}
func (*MockRuntimeServiceServer) PodSandboxStatus(ctx context.Context, req *PodSandboxStatusRequest) (*PodSandboxStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PodSandboxStatus not implemented")
}
func (*MockRuntimeServiceServer) ListPodSandbox(ctx context.Context, req *ListPodSandboxRequest) (*ListPodSandboxResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListPodSandbox not implemented")
}
func (*MockRuntimeServiceServer) StartContainer(ctx context.Context, req *StartContainerRequest) (*StartContainerResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartContainer not implemented")
}
func (*MockRuntimeServiceServer) StopContainer(ctx context.Context, req *StopContainerRequest) (*StopContainerResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StopContainer not implemented")
}
func (*MockRuntimeServiceServer) UpdateContainerResources(ctx context.Context, req *UpdateContainerResourcesRequest) (*UpdateContainerResourcesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateContainerResources not implemented")
}
func (*MockRuntimeServiceServer) ReopenContainerLog(ctx context.Context, req *ReopenContainerLogRequest) (*ReopenContainerLogResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReopenContainerLog not implemented")
}
func (*MockRuntimeServiceServer) ExecSync(ctx context.Context, req *ExecSyncRequest) (*ExecSyncResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ExecSync not implemented")
}
func (*MockRuntimeServiceServer) Exec(ctx context.Context, req *ExecRequest) (*ExecResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Exec not implemented")
}
func (*MockRuntimeServiceServer) Attach(ctx context.Context, req *AttachRequest) (*AttachResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Attach not implemented")
}
func (*MockRuntimeServiceServer) PortForward(ctx context.Context, req *PortForwardRequest) (*PortForwardResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PortForward not implemented")
}
func (*MockRuntimeServiceServer) ContainerStats(ctx context.Context, req *ContainerStatsRequest) (*ContainerStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContainerStats not implemented")
}
func (*MockRuntimeServiceServer) ListContainerStats(ctx context.Context, req *ListContainerStatsRequest) (*ListContainerStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListContainerStats not implemented")
}
func (*MockRuntimeServiceServer) PodSandboxStats(ctx context.Context, req *PodSandboxStatsRequest) (*PodSandboxStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PodSandboxStats not implemented")
}
func (*MockRuntimeServiceServer) ListPodSandboxStats(ctx context.Context, req *ListPodSandboxStatsRequest) (*ListPodSandboxStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListPodSandboxStats not implemented")
}
func (*MockRuntimeServiceServer) UpdateRuntimeConfig(ctx context.Context, req *UpdateRuntimeConfigRequest) (*UpdateRuntimeConfigResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UpdateRuntimeConfig not implemented")
}
func (*MockRuntimeServiceServer) Status(ctx context.Context, req *StatusRequest) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}
func (*MockRuntimeServiceServer) CheckpointContainer(context.Context, *CheckpointContainerRequest) (*CheckpointContainerResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}
func (*MockRuntimeServiceServer) GetContainerEvents(*GetEventsRequest, RuntimeService_GetContainerEventsServer) error {
	return status.Errorf(codes.Unimplemented, "method Status not implemented")
}
func (*MockRuntimeServiceServer) ListMetricDescriptors(context.Context, *ListMetricDescriptorsRequest) (*ListMetricDescriptorsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}
func (*MockRuntimeServiceServer) ListPodSandboxMetrics(context.Context, *ListPodSandboxMetricsRequest) (*ListPodSandboxMetricsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}
func (*MockRuntimeServiceServer) RuntimeConfig(context.Context, *RuntimeConfigRequest) (*RuntimeConfigResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}

func CreateContainer(testName string, deviceIDs []string) (string, string, string, string) {
	var config *ContainerConfig = &ContainerConfig{}
	var testHostPath, testMetricsHostPath, containerConfigHostPath, containerMetricsHostPath string
	if len(deviceIDs) > 0 {
		// emulate Allocate call
		// generate senlib file
		spyreConfigFolder := fmt.Sprintf("%s-%s", spyreconf.SpyreConfigBaseFolderName, testName)
		spyreMetricsFolder := fmt.Sprintf("%s-%s", spyreconf.SpyreMetricBaseFolderName, testName)
		testHostPath = filepath.Join(testHostPath, spyreConfigFolder)
		testMetricsHostPath = filepath.Join(testMetricsHostPath, spyreMetricsFolder)
		mntUuid := testName
		resourcePoolName := "spyre_pf"
		containerConfigHostPath = filepath.Join(testHostPath, mntUuid)
		err := utils.CreateFolderIfNotExists(containerConfigHostPath)
		containerMetricsHostPath = filepath.Join(testMetricsHostPath, mntUuid)
		Expect(err).To(BeNil())
		glog.Infof("generate senlibfile %s", containerConfigHostPath)
		err = senlibConfigGnerator.GenerateConfigFile(resourcePoolName, deviceIDs, containerConfigHostPath)
		Expect(err).To(BeNil())
		readResourcePoolName, err := spyreconf.ReadResourcePool(containerConfigHostPath)
		Expect(err).To(BeNil())
		Expect(readResourcePoolName).To(Equal(resourcePoolName))
		config = &ContainerConfig{
			Mounts: []*Mount{
				{
					ContainerPath: "/etc/aiu",
					HostPath:      containerConfigHostPath,
				},
				{
					ContainerPath: "/tmp/spyre-metrics",
					HostPath:      containerMetricsHostPath,
				},
			},
		}
	}
	// Dial CRI-O's gRPC API over Unix socket
	conn, err := grpc.NewClient(spyrert.GetRuntimeUnixSocketPath(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	Expect(err).To(BeNil())
	defer conn.Close()
	// Create a new CRI-O client
	client := NewRuntimeServiceClient(conn)

	// Define the request to list all containers
	createContainerRequest := &CreateContainerRequest{
		PodSandboxId: uuid.NewString(),
		Config:       config,
	}
	resp, err := client.CreateContainer(context.Background(), createContainerRequest)
	Expect(err).To(BeNil())
	Expect(resp).ToNot(BeNil())
	return testHostPath, containerConfigHostPath, containerMetricsHostPath, resp.ContainerId
}

func DeleteContainer(containerId string) {
	// Dial CRI-O's gRPC API over Unix socket
	conn, err := grpc.NewClient(spyrert.GetRuntimeUnixSocketPath(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	Expect(err).To(BeNil())
	defer conn.Close()
	// Create a new CRI-O client
	client := NewRuntimeServiceClient(conn)
	_, err = client.RemoveContainer(context.Background(), &RemoveContainerRequest{
		ContainerId: containerId,
	})
	Expect(err).To(BeNil())
}
