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

package kcc

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configapis "github.com/kubewharf/katalyst-api/pkg/apis/config/v1alpha1"
	configinformers "github.com/kubewharf/katalyst-api/pkg/client/informers/externalversions/config/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/client/listers/config/v1alpha1"
	kcclient "github.com/kubewharf/katalyst-core/pkg/client"
	"github.com/kubewharf/katalyst-core/pkg/client/control"
	"github.com/kubewharf/katalyst-core/pkg/config/controller"
	"github.com/kubewharf/katalyst-core/pkg/config/generic"
	"github.com/kubewharf/katalyst-core/pkg/consts"
	kcctarget "github.com/kubewharf/katalyst-core/pkg/controller/kcc/target"
	kccutil "github.com/kubewharf/katalyst-core/pkg/controller/kcc/util"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	"github.com/kubewharf/katalyst-core/pkg/util"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

const (
	kccControllerName = "kcc"
)

const (
	kccWorkerCount = 1
)

const (
	kccConditionTypeValidReasonNormal                     = "Normal"
	kccConditionTypeValidReasonPrioritySelectorKeyInvalid = "PrioritySelectorKeyInvalid"
	kccConditionTypeValidReasonTargetTypeOverlap          = "TargetTypeOverlap"
	kccConditionTypeValidReasonTargetTypeNotAllowed       = "TargetTypeNotAllowed"
	kccConditionTypeValidReasonTargetTypeNotExist         = "TargetTypeNotExist"
	kccConditionTypeValidReasonTerminating                = "Terminating"
)

type HaloCustomConfigController struct {
	ctx       context.Context
	dryRun    bool
	kccConfig *controller.KCCConfig

	client              *kcclient.GenericClientSet
	kccControl          control.KCCControl
	unstructuredControl control.UnstructuredControl

	// haloCustomConfigLister can list/get HaloCustomConfig from the shared informer's store
	haloCustomConfigLister v1alpha1.HaloCustomConfigLister
	// haloCustomConfigSyncQueue queue for HaloCustomConfig
	haloCustomConfigSyncQueue workqueue.RateLimitingInterface

	syncedFunc []cache.InformerSynced

	// targetHandler store gvr kcc and gvr
	targetHandler *kcctarget.HaloCustomConfigTargetHandler

	// metricsEmitter for emit metrics
	metricsEmitter metrics.MetricEmitter
}

func NewHaloCustomConfigController(
	ctx context.Context,
	genericConf *generic.GenericConfiguration,
	_ *controller.GenericControllerConfiguration,
	kccConfig *controller.KCCConfig,
	client *kcclient.GenericClientSet,
	haloCustomConfigInformer configinformers.HaloCustomConfigInformer,
	metricsEmitter metrics.MetricEmitter,
	targetHandler *kcctarget.HaloCustomConfigTargetHandler,
) (*HaloCustomConfigController, error) {
	k := &HaloCustomConfigController{
		ctx:                       ctx,
		client:                    client,
		dryRun:                    genericConf.DryRun,
		kccConfig:                 kccConfig,
		haloCustomConfigLister:    haloCustomConfigInformer.Lister(),
		targetHandler:             targetHandler,
		haloCustomConfigSyncQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), kccControllerName),
		syncedFunc: []cache.InformerSynced{
			haloCustomConfigInformer.Informer().HasSynced,
			targetHandler.HasSynced,
		},
	}

	haloCustomConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    k.addHaloCustomConfigEventHandle,
		UpdateFunc: k.updateHaloCustomConfigEventHandle,
	})

	if metricsEmitter == nil {
		k.metricsEmitter = metrics.DummyMetrics{}
	} else {
		k.metricsEmitter = metricsEmitter.WithTags(kccControllerName)
	}

	k.kccControl = control.DummyKCCControl{}
	k.unstructuredControl = control.DummyUnstructuredControl{}
	if !k.dryRun {
		k.kccControl = control.NewRealKCCControl(client.InternalClient)
		k.unstructuredControl = control.NewRealUnstructuredControl(client.DynamicClient)
	}

	// register kcc-target informer handler
	targetHandler.RegisterTargetHandler(kccControllerName, k.haloCustomConfigTargetHandler)
	return k, nil
}

func (k *HaloCustomConfigController) Run() {
	defer utilruntime.HandleCrash()
	defer k.haloCustomConfigSyncQueue.ShutDown()

	defer klog.Infof("shutting down %s controller", kccControllerName)

	if !cache.WaitForCacheSync(k.ctx.Done(), k.syncedFunc...) {
		utilruntime.HandleError(fmt.Errorf("unable to sync caches for %s controller", kccControllerName))
		return
	}
	klog.Infof("caches are synced for %s controller", kccControllerName)
	klog.Infof("start %d workers for %s controller", kccWorkerCount, kccControllerName)

	for i := 0; i < kccWorkerCount; i++ {
		go wait.Until(k.worker, time.Second, k.ctx.Done())
	}

	<-k.ctx.Done()
}

func (k *HaloCustomConfigController) addHaloCustomConfigEventHandle(obj interface{}) {
	t, ok := obj.(*configapis.HaloCustomConfig)
	if !ok {
		klog.Errorf("[kcc] cannot convert obj to *HaloCustomConfig: %v", obj)
		return
	}

	klog.V(4).Infof("[kcc] notice addition of HaloCustomConfig %s", native.GenerateUniqObjectNameKey(t))
	k.enqueueHaloCustomConfig(t)
}

func (k *HaloCustomConfigController) updateHaloCustomConfigEventHandle(_, new interface{}) {
	t, ok := new.(*configapis.HaloCustomConfig)
	if !ok {
		klog.Errorf("[kcc] cannot convert obj to *HaloCustomConfig: %v", new)
		return
	}

	klog.V(4).Infof("[kcc] notice update of HaloCustomConfig %s", native.GenerateUniqObjectNameKey(t))
	k.enqueueHaloCustomConfig(t)
}

func (k *HaloCustomConfigController) enqueueHaloCustomConfig(kcc *configapis.HaloCustomConfig) {
	if kcc == nil {
		klog.Warning("[kcc] trying to enqueue a nil kcc")
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(kcc)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", kcc, err))
		return
	}

	k.haloCustomConfigSyncQueue.Add(key)

	// if this kcc has same gvr with others, we also enqueue them to reconcile
	if kccKeys := k.targetHandler.GetKCCKeyListByGVR(kcc.Spec.TargetType); len(kccKeys) > 1 {
		klog.Infof("[kcc] kcc %s whose target type is overlap with keys: %s", native.GenerateUniqObjectNameKey(kcc), kccKeys)
		for _, otherKey := range kccKeys {
			if key == otherKey {
				continue
			}
			k.haloCustomConfigSyncQueue.Add(otherKey)
		}
	}
}

func (k *HaloCustomConfigController) worker() {
	for k.processNextHaloCustomConfigWorkItem() {
	}
}

func (k *HaloCustomConfigController) processNextHaloCustomConfigWorkItem() bool {
	key, quit := k.haloCustomConfigSyncQueue.Get()
	if quit {
		return false
	}
	defer k.haloCustomConfigSyncQueue.Done(key)

	err := k.syncHaloCustomConfig(key.(string))
	if err == nil {
		k.haloCustomConfigSyncQueue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync kcc %q failed with %v", key, err))
	k.haloCustomConfigSyncQueue.AddRateLimited(key)

	return true
}

func (k *HaloCustomConfigController) syncHaloCustomConfig(key string) error {
	klog.V(4).Infof("[kcc] processing kcc %s", key)
	kcc, err := k.getKCCByKey(key)
	if apierrors.IsNotFound(err) {
		klog.Warningf("[kcc] kcc %s is not found", key)
		return nil
	} else if err != nil {
		klog.Errorf("[kcc] kcc %s get error: %v", key, err)
		return err
	}

	// handle kcc deletion
	if kcc.DeletionTimestamp != nil {
		err := k.handleKCCFinalizer(kcc)
		if err != nil {
			return err
		}
		return nil
	}

	// make sure kcc obj has finalizer to prevent it from being deleted by mistake
	kcc, err = k.ensureKCCFinalizer(kcc)
	if err != nil {
		return err
	}

	// get related kcc keys of gvr
	kccKeys := k.targetHandler.GetKCCKeyListByGVR(kcc.Spec.TargetType)
	if len(kccKeys) == 0 {
		if !k.kccConfig.ValidAPIGroupSet.Has(kcc.Spec.TargetType.Group) {
			// kcc with not allowed target type, of which api group is not allowed
			return k.updateKCCStatusCondition(kcc, configapis.HaloCustomConfigConditionTypeValid, v1.ConditionFalse,
				kccConditionTypeValidReasonTargetTypeNotAllowed, fmt.Sprintf("api group %s of target type %s is not in valid api group set %v",
					kcc.Spec.TargetType.Group, kcc.Spec.TargetType, k.kccConfig.ValidAPIGroupSet))
		}

		// kcc target type is not exist, we will re-enqueue after 30s as
		// crd of the gvr may not have been created yet. Because we not list/watch crd add/update event, we
		// reconcile it periodically to check it whether the gvr is created
		k.haloCustomConfigSyncQueue.AddAfter(key, 30*time.Second)

		return k.updateKCCStatusCondition(kcc, configapis.HaloCustomConfigConditionTypeValid, v1.ConditionFalse,
			kccConditionTypeValidReasonTargetTypeNotExist, fmt.Sprintf("crd of target type %s is not created", kcc.Spec.TargetType))
	} else if len(kccKeys) > 1 {
		// kcc with overlap target type
		// we will check other kcc whether is alive
		aliveKeys := make([]string, 0, len(kccKeys))
		for _, otherKey := range kccKeys {
			if otherKey == key {
				continue
			}

			otherKCC, err := k.getKCCByKey(otherKey)
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}

			if err == nil && otherKCC.GetDeletionTimestamp() == nil {
				aliveKeys = append(aliveKeys, otherKey)
			}
		}

		if len(aliveKeys) > 0 {
			klog.Errorf("[kcc] kcc %s is overlap with other key: %s", native.GenerateUniqObjectNameKey(kcc), aliveKeys)
			return k.updateKCCStatusCondition(kcc, configapis.HaloCustomConfigConditionTypeValid, v1.ConditionFalse,
				kccConditionTypeValidReasonTargetTypeOverlap, fmt.Sprintf("it is overlap with other kcc %v", aliveKeys))
		}
	}

	// check whether kcc node selector allowed key list is valid
	msg, ok := checkNodeLabelSelectorAllowedKeyList(kcc)
	if !ok {
		return k.updateKCCStatusCondition(kcc, configapis.HaloCustomConfigConditionTypeValid, v1.ConditionFalse,
			kccConditionTypeValidReasonPrioritySelectorKeyInvalid, msg)
	}

	targetAccessor, ok := k.targetHandler.GetTargetAccessorByGVR(kcc.Spec.TargetType)
	if !ok {
		return fmt.Errorf("cannot get accessor by gvr %s", kcc.Spec.TargetType.String())
	}

	// get all related targets
	targets, err := targetAccessor.List(labels.Everything())
	if err != nil {
		return err
	}

	// filter all deleting targets
	targets = native.FilterOutDeletingUnstructured(targets)

	// collect all invalid configs
	invalidConfigList, err := k.collectInvalidConfigs(targets)
	if err != nil {
		return err
	}

	oldKCC := kcc.DeepCopy()
	kcc.Status.InvalidTargetConfigList = invalidConfigList
	kcc.Status.ObservedGeneration = kcc.Generation
	setHaloCustomConfigConditions(kcc, configapis.HaloCustomConfigConditionTypeValid, v1.ConditionTrue,
		kccConditionTypeValidReasonNormal, "")
	if !apiequality.Semantic.DeepEqual(oldKCC.Status, kcc.Status) {
		_, err = k.kccControl.UpdateKCCStatus(k.ctx, kcc, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (k *HaloCustomConfigController) getKCCByKey(key string) (*configapis.HaloCustomConfig, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("[kcc] failed to split namespace and name from key %s", key)
		return nil, err
	}

	return k.client.InternalClient.ConfigV1alpha1().HaloCustomConfigs(namespace).Get(k.ctx, name, metav1.GetOptions{
		ResourceVersion: "0",
	})
}

// haloCustomConfigTargetHandler process object of kcc target type from targetAccessor, and
// HaloCustomConfigTargetAccessor will call this handler when some update event on target is added.
func (k *HaloCustomConfigController) haloCustomConfigTargetHandler(gvr metav1.GroupVersionResource, target *unstructured.Unstructured) error {
	for _, syncFunc := range k.syncedFunc {
		if !syncFunc() {
			return fmt.Errorf("[kcc] informer has not synced")
		}
	}

	klog.V(4).Infof("[kcc] gvr: %s, target: %s updated", gvr.String(), native.GenerateUniqObjectNameKey(target))
	if target.GetDeletionTimestamp() != nil {
		err := k.handleKCCTargetFinalizer(gvr, target)
		if err != nil {
			return err
		}
		return nil
	}

	target, err := kccutil.EnsureKCCTargetFinalizer(k.ctx, k.unstructuredControl,
		consts.HaloCustomConfigTargetFinalizerKCC, gvr, target)
	if err != nil {
		return err
	}

	// kcc target updated trigger its kcc to reconcile
	if util.ToKCCTargetResource(target).IsUpdated() {
		kccKeys := k.targetHandler.GetKCCKeyListByGVR(gvr)
		for _, key := range kccKeys {
			k.haloCustomConfigSyncQueue.Add(key)
		}
		return nil
	}

	return nil
}

// handleKCCTargetFinalizer enqueue all related kcc to reconcile when a kcc target was deleted
func (k *HaloCustomConfigController) handleKCCTargetFinalizer(gvr metav1.GroupVersionResource, target *unstructured.Unstructured) error {
	if !controllerutil.ContainsFinalizer(target, consts.HaloCustomConfigTargetFinalizerKCC) {
		return nil
	}

	klog.Infof("[kcc] handling gvr: %s kcc target %s finalizer", gvr.String(), native.GenerateUniqObjectNameKey(target))
	kccKeys := k.targetHandler.GetKCCKeyListByGVR(gvr)
	for _, key := range kccKeys {
		k.haloCustomConfigSyncQueue.Add(key)
	}

	err := kccutil.RemoveKCCTargetFinalizer(k.ctx, k.unstructuredControl, consts.HaloCustomConfigTargetFinalizerKCC, gvr, target)
	if err != nil {
		return err
	}

	klog.Infof("[kcc] success remove gvr: %s kcc target %s finalizer", gvr.String(), native.GenerateUniqObjectNameKey(target))
	return nil
}

func (k *HaloCustomConfigController) collectInvalidConfigs(list []*unstructured.Unstructured) ([]string, error) {
	invalidConfigs := sets.String{}
	for _, o := range list {
		if !util.ToKCCTargetResource(o).CheckValid() {
			invalidConfigs.Insert(native.GenerateUniqObjectNameKey(o))
		}
	}

	return invalidConfigs.List(), nil
}

func (k *HaloCustomConfigController) updateKCCStatusCondition(kcc *configapis.HaloCustomConfig,
	conditionType configapis.HaloCustomConfigConditionType, status v1.ConditionStatus, reason, message string,
) error {
	updated := setHaloCustomConfigConditions(kcc, conditionType, status, reason, message)
	if updated || kcc.Status.ObservedGeneration != kcc.Generation {
		kcc.Status.ObservedGeneration = kcc.Generation
		_, err := k.kccControl.UpdateKCCStatus(k.ctx, kcc, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

// handleKCCFinalizer checks if there still exist CRs for the given kcc
// if true, protect kcc CR from deleting before its configuration CRs been deleted.
func (k *HaloCustomConfigController) handleKCCFinalizer(kcc *configapis.HaloCustomConfig) error {
	if !controllerutil.ContainsFinalizer(kcc, consts.HaloCustomConfigFinalizerKCC) {
		return nil
	}

	accessor, ok := k.targetHandler.GetTargetAccessorByGVR(kcc.Spec.TargetType)
	if ok {
		// only if accessor of gvr exists, we will check its object whether exists
		list, err := accessor.List(labels.Everything())
		if err != nil {
			return err
		}

		if len(list) > 0 {
			residueObjNames := sets.String{}
			for _, o := range list {
				residueObjNames.Insert(native.GenerateUniqObjectNameKey(o))
			}

			return k.updateKCCStatusCondition(kcc, configapis.HaloCustomConfigConditionTypeValid, v1.ConditionFalse,
				kccConditionTypeValidReasonTerminating, fmt.Sprintf("residue configs: %s", residueObjNames.List()))
		}
	}

	err := k.removeKCCFinalizer(kcc)
	if err != nil {
		return err
	}
	return nil
}

func (k *HaloCustomConfigController) ensureKCCFinalizer(kcc *configapis.HaloCustomConfig) (*configapis.HaloCustomConfig, error) {
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err, getErr error
		if controllerutil.ContainsFinalizer(kcc, consts.HaloCustomConfigFinalizerKCC) {
			return nil
		}

		controllerutil.AddFinalizer(kcc, consts.HaloCustomConfigFinalizerKCC)
		newKCC, err := k.kccControl.UpdateKCC(k.ctx, kcc, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			newKCC, getErr = k.client.InternalClient.ConfigV1alpha1().HaloCustomConfigs(kcc.Namespace).Get(k.ctx, kcc.Name, metav1.GetOptions{ResourceVersion: "0"})
			if err != nil {
				return getErr
			}
		}

		kcc = newKCC
		return err
	})
	if err != nil {
		return nil, err
	}

	return kcc, nil
}

func (k *HaloCustomConfigController) removeKCCFinalizer(kcc *configapis.HaloCustomConfig) error {
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err, getErr error
		if !controllerutil.ContainsFinalizer(kcc, consts.HaloCustomConfigFinalizerKCC) {
			return nil
		}

		controllerutil.RemoveFinalizer(kcc, consts.HaloCustomConfigFinalizerKCC)
		newKCC, err := k.kccControl.UpdateKCC(k.ctx, kcc, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			newKCC, getErr = k.client.InternalClient.ConfigV1alpha1().HaloCustomConfigs(kcc.Namespace).Get(k.ctx, kcc.Name, metav1.GetOptions{ResourceVersion: "0"})
			if err != nil {
				return getErr
			}
		}

		kcc = newKCC
		return err
	})
	if err != nil {
		return err
	}

	return nil
}

// checkNodeLabelSelectorAllowedKeyList checks if the priority of NodeLabelSelectorAllowedKeyList is duplicated
func checkNodeLabelSelectorAllowedKeyList(kcc *configapis.HaloCustomConfig) (string, bool) {
	duplicatedPrioritySet := sets.NewInt32()
	priorityKeyListMap := map[int32]bool{}
	for _, priorityAllowedKeyList := range kcc.Spec.NodeLabelSelectorAllowedKeyList {
		if priorityKeyListMap[priorityAllowedKeyList.Priority] {
			duplicatedPrioritySet.Insert(priorityAllowedKeyList.Priority)
			continue
		}
		priorityKeyListMap[priorityAllowedKeyList.Priority] = true
	}

	if len(duplicatedPrioritySet) > 0 {
		return fmt.Sprintf("duplicated priority: %v", duplicatedPrioritySet.List()), false
	}

	return "", true
}

// setHaloCustomConfigConditions is used to set conditions for kcc
func setHaloCustomConfigConditions(
	kcc *configapis.HaloCustomConfig,
	conditionType configapis.HaloCustomConfigConditionType,
	conditionStatus v1.ConditionStatus,
	reason, message string,
) bool {
	var conditionIndex int
	conditions := kcc.Status.Conditions
	for conditionIndex = 0; conditionIndex < len(conditions); conditionIndex++ {
		if conditions[conditionIndex].Type == conditionType {
			break
		}
	}

	if conditionIndex == len(conditions) {
		conditions = append(conditions, configapis.HaloCustomConfigCondition{
			Type: conditionType,
		})
	}

	condition := &conditions[conditionIndex]
	if condition.Status != conditionStatus || condition.Message != message ||
		condition.Reason != reason {
		condition.LastTransitionTime = metav1.NewTime(time.Now())
		condition.Status = conditionStatus
		condition.Reason = reason
		condition.Message = message
		kcc.Status.Conditions = conditions
		return true
	}

	return false
}
