/*
 * +-------------------------------------------------------------------+
 * | (C) Copyright IBM Corp. 2025, 2026                                |
 * | SPDX-License-Identifier: Apache-2.0                               |
 * +-------------------------------------------------------------------+
 */

package config_test

import (
	"os"

	. "github.com/ibm-aiu/spyre-device-plugin/pkg/spyredevice/config"
	"github.com/ibm-aiu/spyre-device-plugin/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Metrics", func() {
	DescribeTable("GetUuidFromPath", func(configHostPath, expected string) {
		uuidValue := utils.GetUuidFromPath(configHostPath)
		Expect(uuidValue).To(Equal(expected))
	},
		Entry("one-level folder", "config-host-path/some-uuid-value", "some-uuid-value"),
		Entry("two-level folder", "top-folder/config-host-path/some-uuid-value", "some-uuid-value"),
		Entry("non-level", "some-uuid-value", "some-uuid-value"),
		Entry("empty", "", ""),
	)

	It("write/read pod info", func() {
		pod := &corev1.Pod{}
		name := "test-pod"
		namespace := "test-ns"
		configHostPath := "config-host-path/some-uuid-value"
		pod.Name = name
		pod.Namespace = namespace
		err := utils.CreateFolderIfNotExists(configHostPath)
		Expect(err).To(BeNil())
		err = WriteInfoFiles(configHostPath, *pod)
		Expect(err).To(BeNil())
		createdPodName, err := utils.ReadStringInFile(configHostPath, PodNameFile)
		Expect(err).To(BeNil())
		Expect(createdPodName).To(Equal(name))
		createdPodNamespace, err := utils.ReadStringInFile(configHostPath, PodNamespaceFile)
		Expect(err).To(BeNil())
		Expect(createdPodNamespace).To(Equal(namespace))
		err = os.RemoveAll(configHostPath)
		Expect(err).To(BeNil())
	})
})
