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
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	portraitLister "github.com/kubewharf/katalyst-api/pkg/client/listers/resourceportrait/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/consts"
	katalyst_base "github.com/kubewharf/katalyst-core/cmd/base"
	"github.com/kubewharf/katalyst-core/pkg/client/control"
	"github.com/kubewharf/katalyst-core/pkg/config/controller"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/common"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/predictor"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/provider/prom"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

type Prediction struct {
	ctx context.Context

	conf *controller.OvercommitConfig

	provider  prom.Interface
	predictor predictor.Interface

	nodeLister             listerv1.NodeLister
	podIndexer             cache.Indexer
	workloadLister         map[schema.GroupVersionResource]cache.GenericLister
	resourcePortraitLister portraitLister.PortraitLister
	syncedFunc             []cache.InformerSynced

	nodeUpdater control.NodeUpdater
	rpUpdater   control.RprUpdater

	metricsEmitter metrics.MetricEmitter

	firstReconcileWorkload bool
}

func NewPredictionController(
	ctx context.Context,
	controlCtx *katalyst_base.GenericContext,
	overcommitConf *controller.OvercommitConfig,
) (*Prediction, error) {
	if overcommitConf == nil || controlCtx.Client == nil {
		return nil, fmt.Errorf("client and overcommitConf can not be nil")
	}

	klog.V(6).Infof("overcommitConf: %v", *overcommitConf)

	predictionController := &Prediction{
		ctx:                    ctx,
		metricsEmitter:         controlCtx.EmitterPool.GetDefaultMetricsEmitter(),
		conf:                   overcommitConf,
		workloadLister:         map[schema.GroupVersionResource]cache.GenericLister{},
		firstReconcileWorkload: false,
	}

	// init workload lister
	workloadInformers := controlCtx.DynamicResourcesManager.GetDynamicInformers()
	for _, wf := range workloadInformers {
		klog.Infof("workload informer: %v", wf.GVR.String())
		predictionController.workloadLister[wf.GVR] = wf.Informer.Lister()
		predictionController.syncedFunc = append(predictionController.syncedFunc, wf.Informer.Informer().HasSynced)
	}

	err := predictionController.initProvider()
	if err != nil {
		klog.Errorf("init provider fail: %v", err)
		return nil, err
	}

	// init predictor
	err = predictionController.initPredictor()
	if err != nil {
		klog.Errorf("init predictor fail: %v", err)
		return nil, err
	}

	predictionController.resourcePortraitLister = controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Lister()
	predictionController.syncedFunc = append(predictionController.syncedFunc, controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Informer().HasSynced)

	podInformer := controlCtx.KubeInformerFactory.Core().V1().Pods()
	predictionController.podIndexer = podInformer.Informer().GetIndexer()
	predictionController.podIndexer.AddIndexers(cache.Indexers{
		nodePodIndex: nodePodIndexFunc,
	})
	predictionController.syncedFunc = append(predictionController.syncedFunc, podInformer.Informer().HasSynced)

	predictionController.nodeLister = controlCtx.KubeInformerFactory.Core().V1().Nodes().Lister()
	predictionController.syncedFunc = append(predictionController.syncedFunc, controlCtx.KubeInformerFactory.Core().V1().Nodes().Informer().HasSynced)

	predictionController.nodeUpdater = control.NewRealNodeUpdater(controlCtx.Client.KubeClient)
	predictionController.rpUpdater = control.NewRealRprUpdater(controlCtx.Client.InternalClient)

	predictionController.addDeleteHandler(workloadInformers)

	return predictionController, nil
}

func (p *Prediction) Run() {
	if !cache.WaitForCacheSync(p.ctx.Done(), p.syncedFunc...) {
		klog.Fatalf("unable to sync caches")
	}

	if p.predictor != nil && p.provider != nil {
		go wait.Until(p.reconcileWorkloads, p.conf.Prediction.PredictPeriod, p.ctx.Done())

		// if predictor is used, wait for first reconcile before reconcile nodes
		_ = wait.PollImmediateUntil(5*time.Second, func() (done bool, err error) {
			return p.firstReconcileWorkload, nil
		}, p.ctx.Done())

		klog.V(6).Infof("first reconcile workloads finish: %v", p.firstReconcileWorkload)

		go wait.Until(p.reconcileNodes, p.conf.Prediction.ReconcilePeriod, p.ctx.Done())
	} else {
		klog.Infof("nil predictor, skip reconcile workload")
		go wait.Until(p.reconcileNodes, p.conf.Prediction.ReconcilePeriod, p.ctx.Done())
	}
}

// calculate node usage and overcommitment ratio by workloads usage
func (p *Prediction) reconcileNodes() {
	// list nodes
	nodeList, err := p.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("overcommit prediction list node fail: %v", err)
		return
	}

	for _, node := range nodeList {
		cpuVal, memoryVal, err := p.estimateNode(node)
		if err != nil {
			klog.Errorf("estimate node %v fail: %v", node.Name, err)
			continue
		}
		klog.V(6).Infof("estimate node %v overcommit CPU: %v, memory: %v", node.Name, cpuVal, memoryVal)

		// update node annotation
		err = p.updatePredictOvercommitRatio(cpuVal, memoryVal, node)
		if err != nil {
			klog.Errorf("update node %v overcommit ratio fail: %v", node.Name, err)
			continue
		}
	}
}

func (p *Prediction) estimateNode(node *v1.Node) (float64, float64, error) {
	// get pods in node
	objs, err := p.podIndexer.ByIndex(nodePodIndex, node.Name)
	if err != nil {
		return 0, 0, err
	}
	if len(objs) <= 0 {
		return 0, 0, nil
	}

	var (
		sumPodCpuTimeSeries, sumPodMemoryTimeSeries *common.TimeSeries
		nodeResource                                = v1.ResourceList{}
	)
	for _, obj := range objs {
		pod, ok := obj.(*v1.Pod)
		if !ok {
			return 0, 0, fmt.Errorf("can not convert obj to pod: %v", obj)
		}
		klog.V(6).Infof("estimate node, namespace: %v, podname: %v, owner: %v", pod.Namespace, pod.Name, pod.OwnerReferences)

		// get pod resource portrait usage
		cpuTimeSeries, memoryTimeSeries, podResource := p.podResourceTimeSeries(pod)

		// sum pod resources
		if sumPodCpuTimeSeries == nil {
			sumPodCpuTimeSeries = cpuTimeSeries
		} else {
			err = sumPodCpuTimeSeries.Add(cpuTimeSeries)
			if err != nil {
				klog.Errorf("estimate node %v fail: %v", node.Name, err)
				return 0, 0, err
			}
		}
		if sumPodMemoryTimeSeries == nil {
			sumPodMemoryTimeSeries = memoryTimeSeries
		} else {
			err = sumPodMemoryTimeSeries.Add(memoryTimeSeries)
			if err != nil {
				klog.Errorf("estimate node %v fail: %v", node.Name, err)
				return 0, 0, err
			}
		}

		nodeResource = native.AddResources(nodeResource, podResource)
	}

	klog.V(6).Infof("node %v cpu resource: %v, memory resource: %v, pod CPU timeSeries: %v, pod memory timeSeries: %v",
		node.Name, nodeResource.Cpu().String(), nodeResource.Memory().String(), sumPodCpuTimeSeries, sumPodMemoryTimeSeries)

	nodeAllocatable := getNodeAllocatable(node)
	// calculate node overcommitment ratio
	cpuOvercommitRatio := p.resourceToOvercommitRatio(
		node.Name,
		v1.ResourceCPU.String(),
		float64(nodeResource.Cpu().MilliValue()),
		sumPodCpuTimeSeries.Max().Value,
		float64(nodeAllocatable.Cpu().MilliValue()))

	memoryOvercommitRatio := p.resourceToOvercommitRatio(
		node.Name,
		v1.ResourceMemory.String(),
		float64(nodeResource.Memory().Value()),
		sumPodMemoryTimeSeries.Max().Value,
		float64(nodeAllocatable.Memory().Value()))

	return cpuOvercommitRatio, memoryOvercommitRatio, nil
}

// return CPU timeSeries, memory timeSeries and requestResource
func (p *Prediction) podResourceTimeSeries(pod *v1.Pod) (*common.TimeSeries, *common.TimeSeries, v1.ResourceList) {
	// pod request
	podResource := native.SumUpPodRequestResources(pod)

	// pod to workload
	workloadName, workloadType, ok := p.podToWorkloadNameAndType(pod)
	if !ok {
		klog.Warningf("get pod %v-%v workload fail", pod.Namespace, pod.Name)
		cpuTs, memoryTs := p.timeSeriesByRequest(podResource)
		return cpuTs, memoryTs, podResource
	}

	// get time series from resource portrait result
	cpuTs := p.getResourcePortraitTimeSeries(pod.Namespace, v1.ResourceCPU.String(), workloadName, workloadType, podResource.Cpu().MilliValue())
	if cpuTs == nil {
		cpuTs = p.cpuTimeSeriesByRequest(podResource)
	}
	klog.V(6).Infof("workload %v timeseries: %v", workloadName, cpuTs.Samples)
	klog.V(6).Infof("pod %v podResource: %v", pod.Name, podResource.Cpu().MilliValue())

	memoryTs := p.getResourcePortraitTimeSeries(pod.Namespace, v1.ResourceMemory.String(), workloadName, workloadType, podResource.Memory().Value())
	if memoryTs == nil {
		memoryTs = p.memoryTimeSeriesByRequest(podResource)
	}

	return cpuTs, memoryTs, podResource
}

func (p *Prediction) podToWorkloadNameAndType(pod *v1.Pod) (string, string, bool) {
	if p.conf.Prediction.TargetReferenceNameKey != "" && p.conf.Prediction.TargetReferenceTypeKey != "" {
		return p.podToWorkloadByLabel(pod)
	}
	return p.podToWorkloadByOwner(pod)
}

func (p *Prediction) podToWorkloadByOwner(pod *v1.Pod) (string, string, bool) {
	for _, owner := range pod.OwnerReferences {
		kind := owner.Kind
		switch kind {
		// resource portrait time series predicted and stored by deployment, but pod owned by rs
		case "ReplicaSet":
			names := strings.Split(owner.Name, "-")
			if len(names) <= 1 {
				klog.Warningf("unexpected rs name: %v", owner.Name)
				return "", "", false
			}
			names = names[0 : len(names)-1]
			return strings.Join(names, "-"), "Deployment", true
		default:
			return owner.Name, kind, true
		}
	}

	return "", "", false
}

// get pod owner name and type by specified label key
func (p *Prediction) podToWorkloadByLabel(pod *v1.Pod) (string, string, bool) {
	if pod.Labels == nil {
		return "", "", false
	}

	workloadName, ok := pod.Labels[p.conf.Prediction.TargetReferenceNameKey]
	if !ok {
		return "", "", false
	}

	workloadType, ok := pod.Labels[p.conf.Prediction.TargetReferenceTypeKey]
	if !ok {
		return "", "", false
	}
	return workloadName, workloadType, ok
}

func nodePodIndexFunc(obj interface{}) ([]string, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok || pod == nil {
		return nil, fmt.Errorf("failed to reflect a obj to pod")
	}

	if pod.Spec.NodeName == "" {
		return []string{}, nil
	}

	return []string{pod.Spec.NodeName}, nil
}

// use request * config.scaleFactor as usage time series if pod resource portrait not exist
func (p *Prediction) timeSeriesByRequest(podResource v1.ResourceList) (*common.TimeSeries, *common.TimeSeries) {
	cpuTs := p.cpuTimeSeriesByRequest(podResource)
	memoryTs := p.memoryTimeSeriesByRequest(podResource)
	return cpuTs, memoryTs
}

func (p *Prediction) cpuTimeSeriesByRequest(podResource v1.ResourceList) *common.TimeSeries {
	timeSeries := &common.TimeSeries{
		Samples: make([]common.Sample, 24, 24),
	}

	if podResource.Cpu() != nil && !podResource.Cpu().IsZero() {
		cpuUsage := native.MultiplyResourceQuantity(v1.ResourceCPU, *podResource.Cpu(), p.conf.Prediction.CPUScaleFactor)
		for i := range timeSeries.Samples {
			timeSeries.Samples[i] = common.Sample{
				Value: float64(cpuUsage.MilliValue()),
			}
		}
	}
	return timeSeries
}

func (p *Prediction) memoryTimeSeriesByRequest(podResource v1.ResourceList) *common.TimeSeries {
	timeSeries := &common.TimeSeries{
		Samples: make([]common.Sample, 24, 24),
	}

	if podResource.Memory() != nil && !podResource.Memory().IsZero() {
		memoryUsage := native.MultiplyResourceQuantity(v1.ResourceMemory, *podResource.Memory(), p.conf.Prediction.MemoryScaleFactor)
		for i := range timeSeries.Samples {
			timeSeries.Samples[i] = common.Sample{
				Value: float64(memoryUsage.Value()),
			}
		}
	}
	return timeSeries
}

func (p *Prediction) getResourcePortraitTimeSeries(podNamespace string, resourceName string, workloadName string, workloadType string, resourceRequest int64) *common.TimeSeries {
	namespace := p.conf.Prediction.ResourcePortraitNamespace
	resultName := resourcePortraitResultName(workloadName, workloadType, podNamespace)
	if namespace == "" {
		namespace = podNamespace
		resultName = resourcePortraitResultName(workloadName, workloadType, "")
	}

	rpResult, err := p.resourcePortraitLister.Portraits(namespace).Get(resultName)
	if err != nil {
		klog.Errorf("get workload %v resource portrait %v fail: %v", workloadName, resultName, err)
		return nil
	}

	res, ok := rpResult.TimeSeries[resourceToPortraitMetrics[resourceName]]
	if !ok {
		klog.Errorf("workload %v resource portrait %v not exist", workloadName, resultName)
		return nil
	}

	klog.V(6).Infof("workload %v portrait res: %v", workloadName, res)

	return itemsToTimeSeries(res, resourceName)
}

func resourcePortraitResultName(workloadName, workloadType string, namespace string) string {
	if namespace != "" {
		workloadName = fmt.Sprintf("%s-%s", namespace, workloadName)
	}
	return strings.ToLower(fmt.Sprintf(portraitNameFmt, portraitAppName, workloadType, workloadName))
}

func itemsToTimeSeries(items []v1alpha1.Item, resourceName string) *common.TimeSeries {
	ts := common.EmptyTimeSeries()
	for i := range items {
		value := items[i].Value
		if resourceName == v1.ResourceCPU.String() {
			value = value * 1000
		}
		ts.Samples = append(ts.Samples, common.Sample{
			Value:     value,
			Timestamp: items[i].Timestamp,
		})
	}

	sort.Sort(common.Samples(ts.Samples))
	return ts
}

func (p *Prediction) resourceToOvercommitRatio(nodeName string, resourceName string, request float64, estimateUsage float64, nodeAllocatable float64) float64 {

	if request == 0 {
		klog.Warningf("node %v request is zero", nodeName)
		return 0
	}
	if estimateUsage == 0 {
		klog.Warningf("node %v estimateUsage is zero", nodeName)
		return 0
	}
	if nodeAllocatable == 0 {
		klog.Errorf("node %v allocatable is zero", nodeName)
		return 0
	}

	nodeMaxLoad := estimateUsage / request
	if nodeMaxLoad > 1 {
		nodeMaxLoad = 1
	}
	var (
		podExpectedLoad, nodeTargetLoad float64
	)
	switch resourceName {
	case v1.ResourceCPU.String():
		podExpectedLoad = p.conf.Prediction.PodEstimatedCPULoad
		nodeTargetLoad = p.conf.Prediction.NodeCPUTargetLoad
	case v1.ResourceMemory.String():
		podExpectedLoad = p.conf.Prediction.PodEstimatedMemoryLoad
		nodeTargetLoad = p.conf.Prediction.NodeMemoryTargetLoad
	default:
		klog.Warningf("unknown resourceName: %v", resourceName)
		return 0
	}
	if nodeMaxLoad < podExpectedLoad {
		nodeMaxLoad = podExpectedLoad
	}

	overcommitRatio := ((nodeAllocatable*nodeTargetLoad-estimateUsage)/nodeMaxLoad + request) / nodeAllocatable

	klog.V(6).Infof("resource %v request: %v, allocatable: %v, usage: %v, targetLoad: %v, nodeMaxLoad: %v, overcommitRatio: %v",
		resourceName, request, nodeAllocatable, estimateUsage, nodeTargetLoad, nodeMaxLoad, overcommitRatio)
	if overcommitRatio < 1.0 {
		overcommitRatio = 1.0
	}
	return overcommitRatio
}

// get node allocatable before overcommit
func getNodeAllocatable(node *v1.Node) v1.ResourceList {
	res := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("0"),
		v1.ResourceMemory: resource.MustParse("0"),
	}

	// get allocatable from node annotation first
	cpu, ok := node.Annotations[consts.NodeAnnotationOriginalAllocatableCPUKey]
	if ok {
		res[v1.ResourceCPU] = resource.MustParse(cpu)
	} else {
		// if no allocatable in node annotation, get allocatable from node resource
		res[v1.ResourceCPU] = *node.Status.Allocatable.Cpu()
	}

	mem, ok := node.Annotations[consts.NodeAnnotationOriginalAllocatableMemoryKey]
	if ok {
		res[v1.ResourceMemory] = resource.MustParse(mem)
	} else {
		res[v1.ResourceMemory] = *node.Status.Allocatable.Memory()
	}
	return res
}

func (p *Prediction) updatePredictOvercommitRatio(cpu, memory float64, node *v1.Node) error {
	nodeCopy := node.DeepCopy()
	nodeAnnotation := nodeCopy.Annotations
	if nodeAnnotation == nil {
		nodeAnnotation = make(map[string]string)
	}

	if cpu < 1 {
		delete(nodeAnnotation, consts.NodeAnnotationPredictCPUOvercommitRatioKey)
	} else {
		nodeAnnotation[consts.NodeAnnotationPredictCPUOvercommitRatioKey] = fmt.Sprintf("%.2f", cpu)
	}
	if memory < 1 {
		delete(nodeAnnotation, consts.NodeAnnotationPredictMemoryOvercommitRatioKey)
	} else {
		nodeAnnotation[consts.NodeAnnotationPredictMemoryOvercommitRatioKey] = fmt.Sprintf("%.2f", memory)
	}

	nodeCopy.Annotations = nodeAnnotation
	return p.nodeUpdater.PatchNode(p.ctx, node, nodeCopy)
}
