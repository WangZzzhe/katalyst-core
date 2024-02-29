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

package node

import (
	"fmt"
	"strconv"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	core "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-api/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

const (
	nodeAllocatableMutatorName = "nodeAllocatableMutator"
)

// WebhookNodeAllocatableMutator mutate node allocatable according to overcommit annotation
type WebhookNodeAllocatableMutator struct{}

func NewWebhookNodeAllocatableMutator() *WebhookNodeAllocatableMutator {
	return &WebhookNodeAllocatableMutator{}
}

func (na *WebhookNodeAllocatableMutator) MutateNode(node *core.Node, admissionRequest *admissionv1beta1.AdmissionRequest) error {
	if admissionv1beta1.Update != admissionRequest.Operation || admissionRequest.SubResource != "status" {
		return nil
	}

	if node == nil {
		err := fmt.Errorf("node is nil")
		klog.Error(err)
		return err
	}

	nodeAnnotations := node.Annotations
	if nodeAnnotations == nil {
		nodeAnnotations = make(map[string]string)
	}

	nodeAnnotations[consts.NodeAnnotationOriginalCapacityCPUKey] = node.Status.Capacity.Cpu().String()
	nodeAnnotations[consts.NodeAnnotationOriginalCapacityMemoryKey] = node.Status.Capacity.Memory().String()
	nodeAnnotations[consts.NodeAnnotationOriginalAllocatableCPUKey] = node.Status.Allocatable.Cpu().String()
	nodeAnnotations[consts.NodeAnnotationOriginalAllocatableMemoryKey] = node.Status.Allocatable.Memory().String()
	node.Annotations = nodeAnnotations

	CPUOvercommitRatio, err := cpuOvercommitRatioValidate(nodeAnnotations)
	if err != nil {
		klog.Errorf("node %v cpu overcommit ratio validate fail, err: %v", node.Name, err)
	} else {
		if CPUOvercommitRatio > 1.0 {
			allocatable := node.Status.Allocatable.Cpu()
			capacity := node.Status.Capacity.Cpu()
			newAllocatable := native.MultiplyResourceQuantity(core.ResourceCPU, *allocatable, CPUOvercommitRatio)
			newCapacity := native.MultiplyResourceQuantity(core.ResourceCPU, *capacity, CPUOvercommitRatio)
			klog.V(6).Infof(
				"node %s %s capacity: %v, allocatable: %v, newCapacity: %v, newAllocatable: %v",
				node.Name, core.ResourceCPU,
				capacity.String(), newCapacity.String(),
				allocatable.String(), newAllocatable.String())
			node.Status.Allocatable[core.ResourceCPU] = newAllocatable
			node.Status.Capacity[core.ResourceCPU] = newCapacity
		}
	}

	memoryOvercommitRatio, err := memOvercommitRatioValidate(nodeAnnotations)
	if err != nil {
		klog.Errorf("node %s memory overcommit ratio validate fail, err: %v", node.Name, err)
	} else {
		if memoryOvercommitRatio > 1.0 {
			allocatable := node.Status.Allocatable.Memory()
			capacity := node.Status.Capacity.Memory()
			newAllocatable := native.MultiplyResourceQuantity(core.ResourceMemory, *allocatable, memoryOvercommitRatio)
			newCapacity := native.MultiplyResourceQuantity(core.ResourceMemory, *capacity, memoryOvercommitRatio)
			klog.V(6).Infof("node %s %s capacity: %v, allocatable: %v, newCapacity: %v, newAllocatable: %v",
				node.Name, core.ResourceMemory,
				capacity.String(), newCapacity.String(),
				allocatable.String(), newAllocatable.String())
			node.Status.Allocatable[core.ResourceMemory] = newAllocatable
			node.Status.Capacity[core.ResourceMemory] = newCapacity
		}
	}

	return nil
}

func (na *WebhookNodeAllocatableMutator) Name() string {
	return nodeAllocatableMutatorName
}

func cpuOvercommitRatioValidate(nodeAnnotation map[string]string) (float64, error) {
	// todo: [wangzhe.21] add realtimeKey after realtime overcommit advisor merged.
	return overcommitRatioValidate(
		nodeAnnotation,
		consts.NodeAnnotationCPUOvercommitRatioKey,
		consts.NodeAnnotationPredictCPUOvercommitRatioKey,
		"tmp")
}

func memOvercommitRatioValidate(nodeAnnotation map[string]string) (float64, error) {
	// todo: [wangzhe.21] add realtimeKey after realtime overcommit advisor merged.
	return overcommitRatioValidate(
		nodeAnnotation,
		consts.NodeAnnotationMemoryOvercommitRatioKey,
		consts.NodeAnnotationPredictMemoryOvercommitRatioKey,
		"tmp",
	)
}

func overcommitRatioValidate(
	nodeAnnotation map[string]string,
	setOvercommitKey, predictOvercommitKey, realtimeOvercommitKey string) (float64, error) {

	// overcommit is not allowed if overcommitRatio is not set by user
	setOvercommitVal, ok := nodeAnnotation[setOvercommitKey]
	if !ok {
		return 1.0, nil
	}

	overcommitRatio, err := strconv.ParseFloat(setOvercommitVal, 64)
	if err != nil {
		return 1.0, err
	}

	predictOvercommitVal, ok := nodeAnnotation[predictOvercommitKey]
	if ok {
		predictOvercommitRatio, err := strconv.ParseFloat(predictOvercommitVal, 64)
		if err != nil {
			klog.Errorf("predict overcommit %s validate fail: %v", predictOvercommitVal, err)
		}
		if predictOvercommitRatio < overcommitRatio {
			overcommitRatio = predictOvercommitRatio
		}
	}

	realtimeOvercommitVal, ok := nodeAnnotation[realtimeOvercommitKey]
	if ok {
		realtimeOvercommitRatio, err := strconv.ParseFloat(realtimeOvercommitVal, 64)
		if err != nil {
			klog.Errorf("realtime overcommit %s validate fail: %v", realtimeOvercommitVal, err)
		}
		if realtimeOvercommitRatio < overcommitRatio {
			overcommitRatio = realtimeOvercommitRatio
		}
	}

	if overcommitRatio < 1.0 {
		err = fmt.Errorf("overcommitRatio should be greater than 1")
		klog.Error(err)
		return 1, nil
	}

	return overcommitRatio, nil
}
