/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */
package server

import (
	"context"

	spyrev1alpha1 "github.com/ibm-aiu/spyre-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

var ReconcileReservation = reconcileReservation

func (w *PodWatcher) GetAllocation(
	ctx context.Context, p corev1.Pod) (
	*spyrev1alpha1.SpyreNodeState, int, *spyrev1alpha1.Allocation, error) {
	return w.getAllocation(ctx, p)
}

func (rs *resourceServer) GetEnvs(deviceIDs []string) map[string]string {
	return rs.getEnvs(deviceIDs)
}
