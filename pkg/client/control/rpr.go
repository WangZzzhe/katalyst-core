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

package control

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	clientset "github.com/kubewharf/katalyst-api/pkg/client/clientset/versioned"
)

type RprUpdater interface {
	PatchRpr(ctx context.Context, namespace string, oldRpr, newRpr *v1alpha1.Portrait) (*v1alpha1.Portrait, error)
	CreateRpr(ctx context.Context, namespace string, rpr *v1alpha1.Portrait) (*v1alpha1.Portrait, error)
	DeleteRpr(ctx context.Context, namespace string, name string) error
}

type DummyRprUpdater struct{}

func (d *DummyRprUpdater) PatchRpr(ctx context.Context, namespace string, oldRpr, newRpr *v1alpha1.Portrait) (*v1alpha1.Portrait, error) {
	return newRpr, nil
}

func (d *DummyRprUpdater) CreateRpr(ctx context.Context, namespace string, rpr *v1alpha1.Portrait) (*v1alpha1.Portrait, error) {
	return nil, nil
}

func (d *DummyRprUpdater) DeleteRpr(ctx context.Context, namespace string, name string) error {
	return nil
}

func NewRealRprUpdater(client clientset.Interface) RprUpdater {
	return &RealRprUpdater{
		client: client,
	}
}

type RealRprUpdater struct {
	client clientset.Interface
}

func (r *RealRprUpdater) PatchRpr(ctx context.Context, namespace string, oldRpr, newRpr *v1alpha1.Portrait) (*v1alpha1.Portrait, error) {
	if oldRpr == nil || newRpr == nil {
		return nil, fmt.Errorf("neither old nor new object can be nil")
	}

	oldData, err := json.Marshal(oldRpr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal oldData for rpr: %v, err: %v", oldRpr.Name, err)
	}
	newData, err := json.Marshal(newRpr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal newData for rpr: %v, err: %v", newRpr.Name, err)
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return nil, err
	}

	return r.client.ResourceportraitV1alpha1().Portraits(namespace).Patch(ctx, newRpr.Name, types.MergePatchType, patchBytes, v1.PatchOptions{})
}

func (r *RealRprUpdater) CreateRpr(ctx context.Context, namespace string, rpr *v1alpha1.Portrait) (*v1alpha1.Portrait, error) {
	if rpr == nil {
		return nil, fmt.Errorf("can not create a nil resource portrait ResourcePortraitResult")
	}

	return r.client.ResourceportraitV1alpha1().Portraits(namespace).Create(ctx, rpr, v1.CreateOptions{})
}

func (r *RealRprUpdater) DeleteRpr(ctx context.Context, namespace string, name string) error {
	return r.client.ResourceportraitV1alpha1().Portraits(namespace).Delete(ctx, name, v1.DeleteOptions{})
}
