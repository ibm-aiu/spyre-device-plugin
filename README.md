# Spyre Device Plugin

[Kubernetes Device Plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/) for IBM Spyre AI accelerator cards.

## Features

### Device Discovery & Management

- Discovers Spyre PCI devices filtered by vendor, device code, driver, and address
- Supports physical function (PF) and virtual function (VF) SR-IOV modes
- Pseudo-device mode for testing without physical hardware

### Allocation

- Implements Kubernetes Device Plugin API (`ListAndWatch`, `Allocate`, `PreferredAllocation`)
- Topology-aware allocation with peer affinity for multi-device requests
- Conflict detection with up to 10 allocation retries
- Device reservation mode (holds allocations without container mounting)
- P2P DMA configuration for multi-device allocations

### Configuration Injection

- Generates per-container configuration from a configurable template
- Injects config (read-only) and metrics (read-write, if applicable) path mounts per allocation
- Provides topology file into the config mount for topology-aware resource pools

### Health Monitoring

- Integrates with `spyre-health-checker` via gRPC health client
- Falls back to PCI-based monitor when health checker is unavailable
- Reports unhealthy devices to kubelet in real time

### Node State & Topology

- Maintains PCI topology from static file or `pcitopo` tool output
- Supports dynamic topology updates during hotplug events
- Updates `SpyreNodeState` CRD with live device interfaces and health conditions

### Pod Lifecycle

- Watches pod events via informer/workqueue
- Writes pod name and namespace into the metrics mount for metric attribution
- Cleans up host path mounts on pod termination or plugin restart

## Repository links

- [Contributing](CONTRIBUTING.md)
- [Maintainers](MAINTAINERS.md)
- [License](LICENSE)
