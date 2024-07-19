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

package target

import (
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	configapis "github.com/kubewharf/katalyst-api/pkg/apis/config/v1alpha1"
	configinformers "github.com/kubewharf/katalyst-api/pkg/client/informers/externalversions/config/v1alpha1"
	kcclient "github.com/kubewharf/katalyst-core/pkg/client"
	"github.com/kubewharf/katalyst-core/pkg/config/controller"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

type HaloCustomConfigTargetHandler struct {
	mu sync.RWMutex

	ctx       context.Context
	client    *kcclient.GenericClientSet
	kccConfig *controller.KCCConfig

	syncedFunc []cache.InformerSynced

	// map gvr to kcc key set; actually, it's invalid to hold more than one kcc for
	// one individual gvr, and should be alerted
	gvrHaloCustomConfigMap map[metav1.GroupVersionResource]sets.String
	// haloCustomConfigGVRMap map kcc key to gvr; since the gvc in kcc may be unexpected changed
	// by cases, store in cache to make sure we can still find ite original mapping
	haloCustomConfigGVRMap map[string]metav1.GroupVersionResource
	// map gvr to kcc target accessor
	targetAccessorMap map[metav1.GroupVersionResource]HaloCustomConfigTargetAccessor
	// targetHandlerFuncMap stores those handler functions for all controllers
	// that are interested in kcc-target changes
	targetHandlerFuncMap map[string]HaloCustomConfigTargetHandlerFunc
}

func NewHaloCustomConfigTargetHandler(ctx context.Context, client *kcclient.GenericClientSet, kccConfig *controller.KCCConfig,
	haloCustomConfigInformer configinformers.HaloCustomConfigInformer,
) *HaloCustomConfigTargetHandler {
	k := &HaloCustomConfigTargetHandler{
		ctx:       ctx,
		client:    client,
		kccConfig: kccConfig,
		syncedFunc: []cache.InformerSynced{
			haloCustomConfigInformer.Informer().HasSynced,
		},
		gvrHaloCustomConfigMap: make(map[metav1.GroupVersionResource]sets.String),
		haloCustomConfigGVRMap: make(map[string]metav1.GroupVersionResource),
		targetHandlerFuncMap:   make(map[string]HaloCustomConfigTargetHandlerFunc),
		targetAccessorMap:      make(map[metav1.GroupVersionResource]HaloCustomConfigTargetAccessor),
	}

	haloCustomConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    k.addHaloCustomConfigEventHandle,
		UpdateFunc: k.updateHaloCustomConfigEventHandle,
		DeleteFunc: k.deleteHaloCustomConfigEventHandle,
	})
	return k
}

// HasSynced whether all cache has synced
func (k *HaloCustomConfigTargetHandler) HasSynced() bool {
	for _, hasSynced := range k.syncedFunc {
		if !hasSynced() {
			return false
		}
	}
	return true
}

func (k *HaloCustomConfigTargetHandler) Run() {
	defer k.shutDown()
	<-k.ctx.Done()
}

// RegisterTargetHandler is used to register handler functions for the given gvr
func (k *HaloCustomConfigTargetHandler) RegisterTargetHandler(name string, handlerFunc HaloCustomConfigTargetHandlerFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.targetHandlerFuncMap[name] = handlerFunc
}

// GetKCCKeyListByGVR get kcc keyList by gvr.
func (k *HaloCustomConfigTargetHandler) GetKCCKeyListByGVR(gvr metav1.GroupVersionResource) []string {
	k.mu.RLock()
	defer k.mu.RUnlock()

	kccKeys, ok := k.gvrHaloCustomConfigMap[gvr]
	if ok {
		return kccKeys.List()
	}
	return nil
}

func (k *HaloCustomConfigTargetHandler) GetTargetAccessorByGVR(gvr metav1.GroupVersionResource) (HaloCustomConfigTargetAccessor, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	accessor, ok := k.targetAccessorMap[gvr]
	if ok {
		return accessor, true
	}
	return nil, false
}

// RangeGVRTargetAccessor is used to walk through all accessors and perform the given function
func (k *HaloCustomConfigTargetHandler) RangeGVRTargetAccessor(f func(gvr metav1.GroupVersionResource, accessor HaloCustomConfigTargetAccessor) bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	for gvr, a := range k.targetAccessorMap {
		ret := f(gvr, a)
		if !ret {
			return
		}
	}
}

func (k *HaloCustomConfigTargetHandler) addHaloCustomConfigEventHandle(obj interface{}) {
	kcc, ok := obj.(*configapis.HaloCustomConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *HaloCustomConfig: %v", obj)
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(kcc)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", kcc, err))
		return
	}

	_, err = k.addOrUpdateGVRAndKCC(kcc.Spec.TargetType, key)
	if err != nil {
		klog.Errorf("cannot convert add or update gvr %s kcc %s: %v", kcc.Spec.TargetType, key, err)
		return
	}
}

func (k *HaloCustomConfigTargetHandler) updateHaloCustomConfigEventHandle(old interface{}, new interface{}) {
	oldKCC, ok := old.(*configapis.HaloCustomConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *HaloCustomConfig: %v", new)
		return
	}

	newKCC, ok := new.(*configapis.HaloCustomConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *HaloCustomConfig: %v", new)
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newKCC)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", newKCC, err))
		return
	}

	_, err = k.addOrUpdateGVRAndKCC(newKCC.Spec.TargetType, key)
	if err != nil {
		klog.Errorf("cannot convert add or update gvr %s kcc %s: %v", newKCC.Spec.TargetType, key, err)
		return
	}

	// if kcc has updated, it needs trigger all related kcc target to reconcile
	if newKCC.GetGeneration() == newKCC.Status.ObservedGeneration &&
		oldKCC.Status.ObservedGeneration != newKCC.Status.ObservedGeneration {
		accessor, ok := k.GetTargetAccessorByGVR(newKCC.Spec.TargetType)
		if ok {
			kccTargets, err := accessor.List(labels.Everything())
			if err != nil {
				klog.Errorf("list gvr %s kcc target failed: %v", newKCC.Spec.TargetType, err)
				return
			}

			for _, target := range kccTargets {
				accessor.Enqueue("", target)
			}
		}
	}
}

func (k *HaloCustomConfigTargetHandler) deleteHaloCustomConfigEventHandle(obj interface{}) {
	kcc, ok := obj.(*configapis.HaloCustomConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *HaloCustomConfig: %v", obj)
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(kcc)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", kcc, err))
		return
	}

	k.deleteGVRAndKCCKey(kcc.Spec.TargetType, key)

	// when some kcc of a gvr has been deleted, we need reconcile its kcc target immediately
	accessor, ok := k.GetTargetAccessorByGVR(kcc.Spec.TargetType)
	if ok {
		kccTargets, err := accessor.List(labels.Everything())
		if err != nil {
			klog.Errorf("list gvr %s kcc target failed: %v", kcc.Spec.TargetType, err)
			return
		}

		for _, target := range kccTargets {
			accessor.Enqueue("", target)
		}
	}
}

// addOrUpdateGVRAndKCC add gvr and kcc key to cache and return current kcc keys which use this gvr.
func (k *HaloCustomConfigTargetHandler) addOrUpdateGVRAndKCC(gvr metav1.GroupVersionResource, key string) (HaloCustomConfigTargetAccessor, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	old, ok := k.haloCustomConfigGVRMap[key]
	if ok && old != gvr {
		k.deleteGVRAndKCCKeyWithoutLock(old, key)
	}

	return k.addGVRAndKCCKeyWithoutLock(gvr, key)
}

// deleteGVRAndKCCKey delete gvr and kcc key, return whether it is empty after delete that
func (k *HaloCustomConfigTargetHandler) deleteGVRAndKCCKey(gvr metav1.GroupVersionResource, key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.deleteGVRAndKCCKeyWithoutLock(gvr, key)
}

func (k *HaloCustomConfigTargetHandler) deleteGVRAndKCCKeyWithoutLock(gvr metav1.GroupVersionResource, key string) {
	kccKeys, ok := k.gvrHaloCustomConfigMap[gvr]
	if ok {
		delete(kccKeys, key)
		delete(k.haloCustomConfigGVRMap, key)
	}

	if len(kccKeys) == 0 {
		if accessor, ok := k.targetAccessorMap[gvr]; ok {
			accessor.Stop()
		}
		delete(k.targetAccessorMap, gvr)
		delete(k.gvrHaloCustomConfigMap, gvr)
	}
}

func (k *HaloCustomConfigTargetHandler) addGVRAndKCCKeyWithoutLock(gvr metav1.GroupVersionResource, key string) (HaloCustomConfigTargetAccessor, error) {
	if err := k.checkGVRValid(gvr); err != nil {
		return nil, err
	}

	_, ok := k.gvrHaloCustomConfigMap[gvr]
	if !ok {
		_, ok := k.targetAccessorMap[gvr]
		if !ok {
			accessor, err := NewRealHaloCustomConfigTargetAccessor(gvr,
				k.client.DynamicClient, k.targetHandlerFuncMap)
			if err != nil {
				return nil, err
			}
			accessor.Start()
			k.targetAccessorMap[gvr] = accessor
		} else {
			klog.Fatalf("gvr of targetAccessorMap %s not exist", gvr.String())
		}
		k.gvrHaloCustomConfigMap[gvr] = sets.NewString()
	}
	k.gvrHaloCustomConfigMap[gvr].Insert(key)
	k.haloCustomConfigGVRMap[key] = gvr
	return k.targetAccessorMap[gvr], nil
}

// checkGVRValid is used to check whether the given gvr is valid, skip to create corresponding
// target accessor otherwise
func (k *HaloCustomConfigTargetHandler) checkGVRValid(gvr metav1.GroupVersionResource) error {
	if !k.kccConfig.ValidAPIGroupSet.Has(gvr.Group) {
		return fmt.Errorf("gvr %s is not in valid api group set", gvr.String())
	}

	schemaGVR := native.ToSchemaGVR(gvr.Group, gvr.Version, gvr.Resource)
	resourceList, err := k.client.DiscoveryClient.ServerResourcesForGroupVersion(schemaGVR.GroupVersion().String())
	if err != nil {
		return err
	}

	for _, resource := range resourceList.APIResources {
		if resource.Name == gvr.Resource {
			return nil
		}
	}

	return apierrors.NewNotFound(schemaGVR.GroupResource(), schemaGVR.Resource)
}

func (k *HaloCustomConfigTargetHandler) shutDown() {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, accessor := range k.targetAccessorMap {
		accessor.Stop()
	}

	// clear all maps
	k.targetAccessorMap = make(map[metav1.GroupVersionResource]HaloCustomConfigTargetAccessor)
	k.gvrHaloCustomConfigMap = make(map[metav1.GroupVersionResource]sets.String)
	k.haloCustomConfigGVRMap = make(map[string]metav1.GroupVersionResource)
}
