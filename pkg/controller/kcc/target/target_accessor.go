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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

const (
	targetWorkerCount = 1
)

// HaloCustomConfigTargetAccessor is to handle creation/update/delete event of target unstructured obj,
// and it can trigger obj re-sync by calling Enqueue function
type HaloCustomConfigTargetAccessor interface {
	// Start to reconcile obj of kcc target type
	Start()

	// Stop reconcile obj of kcc target type
	Stop()

	// Enqueue obj of kcc target type to the work queue
	Enqueue(name string, obj *unstructured.Unstructured)

	// List all obj (with DeepCopy) of kcc target type according selector
	List(selector labels.Selector) ([]*unstructured.Unstructured, error)

	// Get obj (with DeepCopy) of kcc target type by namespace and name
	Get(namespace, name string) (*unstructured.Unstructured, error)
}

type DummyHaloCustomConfigTargetAccessor struct{}

func (d DummyHaloCustomConfigTargetAccessor) Start()                               {}
func (d DummyHaloCustomConfigTargetAccessor) Stop()                                {}
func (d DummyHaloCustomConfigTargetAccessor) Enqueue(_ *unstructured.Unstructured) {}
func (d DummyHaloCustomConfigTargetAccessor) List(_ labels.Selector) ([]*unstructured.Unstructured, error) {
	return nil, nil
}

func (d DummyHaloCustomConfigTargetAccessor) Get(_, _ string) (*unstructured.Unstructured, error) {
	return nil, nil
}

// HaloCustomConfigTargetHandlerFunc func to process the obj in the work queue
type HaloCustomConfigTargetHandlerFunc func(gvr metav1.GroupVersionResource, target *unstructured.Unstructured) error

// targetHandlerFuncWithSyncQueue is used to store the handler and
// syncing queue for each kcc-target
type targetHandlerFuncWithSyncQueue struct {
	targetHandlerFunc HaloCustomConfigTargetHandlerFunc
	syncQueue         workqueue.RateLimitingInterface
}

type RealHaloCustomConfigTargetAccessor struct {
	stopCh chan struct{}
	ctx    context.Context

	gvr metav1.GroupVersionResource

	// targetLister can list/get target resource from the targetInformer's store
	targetLister   cache.GenericLister
	targetInformer cache.SharedIndexInformer

	// targetHandlerFuncWithSyncQueueMap is used to store the handler and syncing
	// queue for each kcc-target
	targetHandlerFuncWithSyncQueueMap map[string]targetHandlerFuncWithSyncQueue
}

// NewRealHaloCustomConfigTargetAccessor returns a new HaloCustomConfigTargetAccessor
// which is used to handle creation/update/delete event of target unstructured obj, and it can
// trigger obj re-sync by calling Enqueue function of the returned accessor.
func NewRealHaloCustomConfigTargetAccessor(
	gvr metav1.GroupVersionResource,
	client dynamic.Interface,
	handlerInfos map[string]HaloCustomConfigTargetHandlerFunc,
) (*RealHaloCustomConfigTargetAccessor, error) {
	dynamicInformer := dynamicinformer.NewFilteredDynamicInformer(client,
		native.ToSchemaGVR(gvr.Group, gvr.Version, gvr.Resource),
		metav1.NamespaceAll,
		time.Hour*24,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil)

	k := &RealHaloCustomConfigTargetAccessor{
		stopCh:                            make(chan struct{}),
		gvr:                               gvr,
		targetLister:                      dynamicInformer.Lister(),
		targetInformer:                    dynamicInformer.Informer(),
		targetHandlerFuncWithSyncQueueMap: make(map[string]targetHandlerFuncWithSyncQueue),
	}

	k.targetInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    k.addTargetEventHandle,
		UpdateFunc: k.updateTargetEventHandle,
		DeleteFunc: k.deleteTargetEventHandle,
	})

	for name, info := range handlerInfos {
		k.targetHandlerFuncWithSyncQueueMap[name] = targetHandlerFuncWithSyncQueue{
			targetHandlerFunc: info,
			syncQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(),
				name+"-"+gvr.Resource),
		}
	}

	return k, nil
}

func (k *RealHaloCustomConfigTargetAccessor) Start() {
	// run target informer
	go k.targetInformer.Run(k.stopCh)

	for _, info := range k.targetHandlerFuncWithSyncQueueMap {
		for i := 0; i < targetWorkerCount; i++ {
			go wait.Until(k.generateWorker(info), time.Second, k.stopCh)
		}
	}

	klog.Infof("target accessor of %s has been started", k.gvr.String())
}

func (k *RealHaloCustomConfigTargetAccessor) Stop() {
	klog.Infof("target accessor of %s is stopping", k.gvr.String())

	for _, info := range k.targetHandlerFuncWithSyncQueueMap {
		info.syncQueue.ShutDown()
	}

	close(k.stopCh)
}

// Enqueue will add the obj to the work queue of the target handler, if name is empty,
// it will add the obj to all the work queue of the target handler
func (k *RealHaloCustomConfigTargetAccessor) Enqueue(name string, obj *unstructured.Unstructured) {
	if len(name) == 0 {
		k.enqueueTarget(obj)
		return
	}

	if obj == nil {
		klog.Warning("trying to enqueue a nil kcc target")
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", obj, err))
		return
	}

	info, ok := k.targetHandlerFuncWithSyncQueueMap[name]
	if ok {
		info.syncQueue.Add(key)
	} else {
		klog.Warningf("target handler %s not found", name)
	}
}

func (k *RealHaloCustomConfigTargetAccessor) Get(namespace, name string) (*unstructured.Unstructured, error) {
	if !k.targetInformer.HasSynced() {
		return nil, fmt.Errorf("target targetInformer for %s not synced", k.gvr)
	}

	if namespace == "" {
		obj, err := k.targetLister.Get(name)
		if err != nil {
			return nil, err
		}
		return obj.(*unstructured.Unstructured), nil
	}

	obj, err := k.targetLister.ByNamespace(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return obj.(*unstructured.Unstructured).DeepCopy(), nil
}

func (k *RealHaloCustomConfigTargetAccessor) List(selector labels.Selector) ([]*unstructured.Unstructured, error) {
	if !k.targetInformer.HasSynced() {
		return nil, fmt.Errorf("target targetInformer for %s not synced", k.gvr)
	}

	list, err := k.targetLister.List(selector)
	if err != nil {
		return nil, err
	}

	ret := make([]*unstructured.Unstructured, 0, len(list))
	for _, o := range list {
		ret = append(ret, o.(*unstructured.Unstructured).DeepCopy())
	}
	return ret, nil
}

func (k *RealHaloCustomConfigTargetAccessor) addTargetEventHandle(obj interface{}) {
	t, ok := obj.(*unstructured.Unstructured)
	if !ok {
		klog.Errorf("cannot convert obj to *unstructured.Unstructured: %v", obj)
		return
	}

	klog.V(4).Infof("notice addition of %s, %s", k.gvr, native.GenerateUniqObjectNameKey(t))
	k.enqueueTarget(t)
}

func (k *RealHaloCustomConfigTargetAccessor) updateTargetEventHandle(_, new interface{}) {
	t, ok := new.(*unstructured.Unstructured)
	if !ok {
		klog.Errorf("cannot convert obj to *unstructured.Unstructured: %v", new)
		return
	}

	klog.V(4).Infof("notice update of %s, %s", k.gvr, native.GenerateUniqObjectNameKey(t))
	k.enqueueTarget(t)
}

func (k *RealHaloCustomConfigTargetAccessor) deleteTargetEventHandle(obj interface{}) {
	t, ok := obj.(*unstructured.Unstructured)
	if !ok {
		klog.Errorf("cannot convert obj to *unstructured.Unstructured: %v", obj)
		return
	}

	klog.V(4).Infof("notice delete of %s, %s", k.gvr, native.GenerateUniqObjectNameKey(t))
	k.enqueueTarget(t)
}

func (k *RealHaloCustomConfigTargetAccessor) enqueueTarget(obj *unstructured.Unstructured) {
	if obj == nil {
		klog.Warning("trying to enqueue a nil kcc target")
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", obj, err))
		return
	}

	for _, info := range k.targetHandlerFuncWithSyncQueueMap {
		info.syncQueue.Add(key)
	}
}

func (k *RealHaloCustomConfigTargetAccessor) generateWorker(queue targetHandlerFuncWithSyncQueue) func() {
	return func() {
		for k.processNextHaloCustomConfigTargetItem(queue.syncQueue, queue.targetHandlerFunc) {
		}
	}
}

func (k *RealHaloCustomConfigTargetAccessor) processNextHaloCustomConfigTargetItem(queue workqueue.RateLimitingInterface, handler HaloCustomConfigTargetHandlerFunc) bool {
	key, quit := queue.Get()
	if quit {
		return false
	}
	defer queue.Done(key)

	err := k.syncHandler(key.(string), handler)
	if err == nil {
		queue.Forget(key)
		return true
	}

	klog.Errorf("sync kcc target %q failed with %v", key, err)
	queue.AddRateLimited(key)

	return true
}

func (k *RealHaloCustomConfigTargetAccessor) syncHandler(key string, handlerFunc HaloCustomConfigTargetHandlerFunc) error {
	if !k.targetInformer.HasSynced() {
		return fmt.Errorf("target targetInformer for %s not synced", k.gvr)
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("failed to split namespace and name from key %s", key)
		return err
	}

	target, err := k.Get(namespace, name)
	if apierrors.IsNotFound(err) {
		klog.Warningf("%s resource %s is not found", k.gvr.String(), key)
		return nil
	} else if err != nil {
		klog.Errorf("%s resource %s get error: %v", k.gvr.String(), key, err)
		return err
	}

	return handlerFunc(k.gvr, target)
}
