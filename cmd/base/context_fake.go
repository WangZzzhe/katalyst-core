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

package katalyst_base

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	fakedisco "k8s.io/client-go/discovery/fake"
	coretesting "k8s.io/client-go/testing"
)

func nilObjectFilter(object []runtime.Object) []runtime.Object {
	objects := make([]runtime.Object, 0)
	for _, o := range object {
		if reflect.ValueOf(o).IsNil() {
			continue
		}
		objects = append(objects, o)
	}
	return objects
}

var fakeDiscoveryClient = &fakedisco.FakeDiscovery{Fake: &coretesting.Fake{
	Resources: []*metav1.APIResourceList{
		{
			GroupVersion: appsv1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				{Name: "replicasets", Namespaced: true, Kind: "Replica"},
				{Name: "statefulsets", Namespaced: true, Kind: "StatefulSet"},
			},
		},
		{
			GroupVersion: v1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{Name: "pods", Namespaced: true, Kind: "Pod"},
			},
		},
		//{
		//	GroupVersion: v1alpha1.SchemeGroupVersion.String(),
		//	APIResources: []metav1.APIResource{
		//		{Name: v1alpha1.ResourceNameAdminQoSConfigurations, Namespaced: true, Kind: crd.ResourceKindAdminQoSConfiguration},
		//	},
		//},
	},
}}

// versionedUpdate increases object resource version if needed;
// and if the resourceVersion of is not equal to origin one, returns a conflict error
func versionedUpdate(tracker coretesting.ObjectTracker, gvr schema.GroupVersionResource,
	obj runtime.Object, ns string) error {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return fmt.Errorf("failed to get accessor for object: %v", err)
	}

	if accessor.GetName() == "" {
		return apierrors.NewInvalid(
			obj.GetObjectKind().GroupVersionKind().GroupKind(),
			accessor.GetName(),
			field.ErrorList{field.Required(field.NewPath("metadata.name"), "name is required")})
	}

	oldObject, err := tracker.Get(gvr, ns, accessor.GetName())
	if err != nil {
		return err
	}

	oldAccessor, err := meta.Accessor(oldObject)
	if err != nil {
		return err
	}

	// if the new object does not have the resource version set, and it allows unconditional update,
	// default it to the resource version of the existing resource
	if accessor.GetResourceVersion() == "" {
		accessor.SetResourceVersion(oldAccessor.GetResourceVersion())
	}
	if accessor.GetResourceVersion() != oldAccessor.GetResourceVersion() {
		return apierrors.NewConflict(gvr.GroupResource(), accessor.GetName(), errors.New("object was modified"))
	}

	return increaseObjectResourceVersion(tracker, gvr, obj, ns)
}

// increaseObjectResourceVersion increase object resourceVersion if needed, and if the resourceVersion is empty just skip it
func increaseObjectResourceVersion(tracker coretesting.ObjectTracker, gvr schema.GroupVersionResource,
	obj runtime.Object, ns string) error {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return fmt.Errorf("failed to get accessor for object: %v", err)
	}

	if accessor.GetResourceVersion() == "" {
		accessor.SetResourceVersion("0")
	}

	intResourceVersion, err := strconv.ParseUint(accessor.GetResourceVersion(), 10, 64)
	if err != nil {
		return fmt.Errorf("can not convert resourceVersion %q to int: %v", accessor.GetResourceVersion(), err)
	}
	intResourceVersion++
	accessor.SetResourceVersion(strconv.FormatUint(intResourceVersion, 10))

	return tracker.Update(gvr, obj, ns)
}

// versionedUpdateReactor is reactor for versioned update
func versionedUpdateReactor(tracker coretesting.ObjectTracker,
	action coretesting.UpdateActionImpl) (bool, runtime.Object, error) {
	obj := action.GetObject()
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return true, nil, err
	}

	// if resource version is empty just fallback to default logic
	if accessor.GetResourceVersion() == "" {
		return false, nil, nil
	}

	err = versionedUpdate(tracker, action.GetResource(), obj, action.GetNamespace())
	if err != nil {
		return true, nil, err
	}

	obj, err = tracker.Get(action.GetResource(), action.GetNamespace(), accessor.GetName())
	return true, obj, err
}

// versionedPatchReactor is reactor for versioned patch
func versionedPatchReactor(tracker coretesting.ObjectTracker,
	action coretesting.PatchActionImpl) (bool, runtime.Object, error) {
	obj, err := tracker.Get(action.GetResource(), action.GetNamespace(), action.GetName())
	if err != nil {
		return true, nil, err
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return true, nil, err
	}

	// if resource version is empty just fallback to default logic
	if accessor.GetResourceVersion() == "" {
		return false, nil, nil
	}

	// increases object resourceVersion even if no field update happen
	err = increaseObjectResourceVersion(tracker, action.GetResource(), obj, action.GetNamespace())
	if err != nil {
		return true, nil, err
	}

	// return false to continue the next real patch reactor
	return false, nil, nil
}
