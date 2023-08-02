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
	defaultOvercommitRatio = 1
)

// WebhookNodeAllocatableMutator mutate node allocatable according to overcommit annotation
type WebhookNodeAllocatableMutator struct{}

func NewWebhookNodeAllocatableMutator() *WebhookNodeAllocatableMutator {
	return &WebhookNodeAllocatableMutator{}
}

func (na *WebhookNodeAllocatableMutator) MutateNode(node *core.Node, admissionRequest *admissionv1beta1.AdmissionRequest) error {
	if admissionv1beta1.Update != admissionRequest.Operation {
		return nil
	}

	if node == nil {
		err := fmt.Errorf("node is nil")
		klog.Error(err)
		return err
	}

	if node.Annotations == nil {
		// ignore if node annotation is nil
		return nil
	}

	CPUOvercommitRatioValue, ok := node.Annotations[consts.NodeAnnotationCPUOvercommitRatioKey]
	if ok {
		CPUOvercommitRatio, err := overcommitRatioValidate(CPUOvercommitRatioValue)
		if err != nil {
			klog.Errorf("node %s %s validate fail, value: %s, err: %v", node.Name, consts.NodeAnnotationCPUOvercommitRatioKey, CPUOvercommitRatioValue, err)
		} else {
			quantity := node.Status.Allocatable.Cpu()
			newQuantity := native.MultiplyResourceQuantity(core.ResourceCPU, *quantity, CPUOvercommitRatio)
			klog.V(6).Infof("node %s %s quantity: %v, newQuantity: %v", node.Name, core.ResourceCPU, quantity.String(), newQuantity.String())
			node.Status.Allocatable[core.ResourceCPU] = newQuantity
		}
	}

	memoryOvercommitRatioValue, ok := node.Annotations[consts.NodeAnnotationMemoryOvercommitRatioKey]
	if ok {
		memoryOvercommitRatio, err := overcommitRatioValidate(memoryOvercommitRatioValue)
		if err != nil {
			klog.Errorf("node %s %s validate fail, value: %s, err: %v", node.Name, consts.NodeAnnotationMemoryOvercommitRatioKey, memoryOvercommitRatioValue, err)
		} else {
			quantity := node.Status.Allocatable.Memory()
			newQuantity := native.MultiplyResourceQuantity(core.ResourceMemory, *quantity, memoryOvercommitRatio)
			klog.V(6).Infof("node %s %s quantity: %v, newQuantity: %v", node.Name, core.ResourceMemory, quantity.String(), newQuantity.String())
			node.Status.Allocatable[core.ResourceMemory] = newQuantity
		}
	}

	return nil
}

func overcommitRatioValidate(overcommitRatioAnnotation string) (float64, error) {
	overcommitRatio, err := strconv.ParseFloat(overcommitRatioAnnotation, 64)
	if err != nil {
		return defaultOvercommitRatio, err
	}

	if overcommitRatio < 1.0 {
		err = fmt.Errorf("overcommitRatio should be greater than 1")
		return defaultOvercommitRatio, err
	}

	return overcommitRatio, nil
}
