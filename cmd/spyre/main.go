/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
//
// Copyright 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"github.com/ibm-aiu/spyre-device-plugin/cmd/spyre/manager"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/resources/server"
	spyreclient "github.com/ibm-aiu/spyre-operator/pkg/client"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	defaultConfig = "/etc/aiu/config.json"
)

// Parse Command line flags
func flagInit(cp *manager.CliParams) {
	flag.StringVar(&cp.ConfigFile, "config-file", defaultConfig,
		"JSON device pool config file location")
	flag.StringVar(&cp.ResourcePrefix, "resource-prefix", "ibm.com",
		"resource name prefix used for K8s extended resource")
	flag.BoolVar(&cp.Insecure, "insecure", true,
		"disable TLS for health checker gRPC connection (default: true, insecure mode)")
}

func main() {
	cp := &manager.CliParams{}
	flagInit(cp)
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	zlogger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(zlogger)

	glog.V(1).Infof("loglevel: debug")
	glog.V(1).Infof("Applying configuration in SpyreClusterPolicy resource")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		glog.Errorf("error getting cluster config %v", err)
		return
	}

	spyreClient, err := spyreclient.NewClient(context.Background(), cfg)
	if err != nil {
		glog.Errorf("failed to create an Spyre resource client: %v", err)
		return
	}

	rm := manager.NewResourceManager(cp, spyreClient)

	var podWatcher *server.PodWatcher
	if podWatcher, err = server.NewPodWatcher(cfg, rm.GetAllocateCh(), rm.GetMountedCh(), rm.GetDeallocateCh()); err != nil {
		glog.Errorf("error creating deallocator %v", err)
		return
	}

	glog.V(1).Infof("resource manager reading configs")
	if err := rm.ReadConfig(); err != nil {
		glog.Errorf("error getting resources from file %v", err)
		return
	}
	// Validate configs
	if !rm.ValidConfigs() {
		glog.Fatalf("Exiting.. one or more invalid configuration(s) given")
		return
	}
	glog.V(1).Infof("Discovering host devices")
	if err := rm.DiscoverHostDevices(); err != nil {
		glog.Errorf("error discovering host devices%v", err)
		return
	}

	glog.V(1).Infof("Initializing resource servers")
	if err := rm.InitServers(); err != nil {
		glog.Errorf("error initializing resource servers %v", err)
		return
	}

	podWatcher.NotifyInitialAllocationList()
	podWatcher.Start()
	defer podWatcher.Stop()

	glog.V(1).Infof("Starting all servers...")
	if err := rm.StartAllServers(); err != nil {
		glog.Errorf("error starting resource servers %v\n", err)
		return
	}
	glog.Infof("All servers started.")

	quit := make(chan interface{})
	defer close(quit)
	ctx := context.Background()
	rm.StartSpyreNodeStateUpdateTicker(ctx, cfg, spyreClient, quit)

	glog.V(1).Infof("Listening for term signals")
	// respond to syscalls for termination
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Catch termination signals
	sig := <-sigCh
	glog.Infof("Received signal \"%v\", shutting down.", sig)
	if err := rm.StopAllServers(); err != nil {
		glog.Errorf("stopping servers produced error: %s", err.Error())
	}
}
