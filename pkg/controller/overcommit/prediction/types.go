/*
Copyright 2022 The Katalyst Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package prediction

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
)

const (
	PostRegMatchesPodDeployment  = `[a-z0-9]*-[a-z0-9]{5}$`
	PostRegMatchesPodDaemonSet   = `[a-z0-9]{5}$`
	PostRegMatchesPodStatefulset = `[0-9]*$`
	PostRegMatchesPodDefault     = `.*`
)

const (
	portraitAppName = "overcommit"
)

const (
	namespaceMatchKey = "namespace"
	podMatchKey       = "pod"
	containerMatchKey = "container"
)

const (
	defaultStep = 60 * time.Second
)

const (
	nodePodIndex = "overcommit-nodepod-index"

	portraitNameFmt              = "auto-created-%s-%s-%s-0" // auto-created-{appName}-{workloadType}-{workloadName}-index
	portraitManagedLabelKey      = "rp.katalyst.kubewharf.io/auto-managed"
	portraitManagedLabelValFalse = "false"
)

func podNameByWorkload(workloadName string, workloadType string) string {
	switch workloadType {
	case "Deployment":
		return fmt.Sprintf("^%s-%s", workloadName, PostRegMatchesPodDeployment)
	case "DaemonSet":
		return fmt.Sprintf("^%s-%s", workloadName, PostRegMatchesPodDaemonSet)
	case "StatefulSet":
		return fmt.Sprintf("^%s-%s", workloadName, PostRegMatchesPodStatefulset)
	default:
		return fmt.Sprintf("^%s-%s", workloadName, PostRegMatchesPodDefault)
	}
}

var resourceToPortraitMetrics = map[string]string{
	v1.ResourceCPU.String():    "cpu_utilization_usage_seconds_max",
	v1.ResourceMemory.String(): "memory_utilization_max",
}
