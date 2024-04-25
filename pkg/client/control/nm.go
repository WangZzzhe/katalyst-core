package control

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	"github.com/kubewharf/katalyst-api/pkg/apis/node/v1alpha1"
	clientset "github.com/kubewharf/katalyst-api/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NmUpdater interface {
	CreateNm(ctx context.Context, nm *v1alpha1.NodeMonitor, opts metav1.CreateOptions) (*v1alpha1.NodeMonitor, error)

	DeleteNm(ctx context.Context, nmName string, opts metav1.DeleteOptions) error

	PatchNm(ctx context.Context, nmName string, oldNm, newNm *v1alpha1.NodeMonitor) (*v1alpha1.NodeMonitor, error)

	UpdateNmStatus(ctx context.Context, nm *v1alpha1.NodeMonitor, opts metav1.UpdateOptions) (*v1alpha1.NodeMonitor, error)
}

type DummyNmControl struct{}

func (d DummyNmControl) CreateNm(_ context.Context, nm *v1alpha1.NodeMonitor, _ metav1.CreateOptions) (*v1alpha1.NodeMonitor, error) {
	return nm, nil
}

func (d DummyNmControl) DeleteNm(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return nil
}

func (d DummyNmControl) PatchNm(_ context.Context, _ string, _, newNm *v1alpha1.NodeMonitor) (*v1alpha1.NodeMonitor, error) {
	return newNm, nil
}

func (d DummyNmControl) UpdateNmStatus(_ context.Context, nm *v1alpha1.NodeMonitor, _ metav1.UpdateOptions) (*v1alpha1.NodeMonitor, error) {
	return nm, nil
}

type RealNmControl struct {
	client clientset.Interface
}

func (r *RealNmControl) CreateNm(ctx context.Context, nm *v1alpha1.NodeMonitor, opts metav1.CreateOptions) (*v1alpha1.NodeMonitor, error) {
	if nm == nil {
		return nil, fmt.Errorf("can't create a nil nm")
	}

	return r.client.NodeV1alpha1().NodeMonitors().Create(ctx, nm, opts)
}

func (r *RealNmControl) DeleteNm(ctx context.Context, nmName string, opts metav1.DeleteOptions) error {
	return r.client.NodeV1alpha1().NodeMonitors().Delete(ctx, nmName, opts)
}

func (r *RealNmControl) PatchNm(ctx context.Context, nmName string, oldNm, newNm *v1alpha1.NodeMonitor) (*v1alpha1.NodeMonitor, error) {
	patchBytes, err := createMergePatch(oldNm, newNm)
	if err != nil {
		return nil, fmt.Errorf("createMergePatch fail: %v", err)
	}

	return r.client.NodeV1alpha1().NodeMonitors().Patch(ctx, nmName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
}

func (r *RealNmControl) UpdateNmStatus(ctx context.Context, nm *v1alpha1.NodeMonitor, opts metav1.UpdateOptions) (*v1alpha1.NodeMonitor, error) {
	if nm == nil {
		return nil, fmt.Errorf("can't update a nil nm's status")
	}

	return r.client.NodeV1alpha1().NodeMonitors().UpdateStatus(ctx, nm, opts)
}

func createMergePatch(original, new interface{}) ([]byte, error) {
	pvByte, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	cloneByte, err := json.Marshal(new)
	if err != nil {
		return nil, err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(pvByte, cloneByte, original)
	if err != nil {
		return nil, err
	}
	return patch, nil
}

func NewRealNmUpdater(client clientset.Interface) *RealNmControl {
	return &RealNmControl{
		client: client,
	}
}
