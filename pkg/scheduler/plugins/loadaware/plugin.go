package loadaware

import (
	"context"
	"fmt"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kubewharf/katalyst-api/pkg/apis/node/v1alpha1"
	v1alpha12 "github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config"
	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config/validation"
	listers "github.com/kubewharf/katalyst-api/pkg/client/listers/node/v1alpha1"
	portraitlisters "github.com/kubewharf/katalyst-api/pkg/client/listers/resourceportrait/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

const (
	Name = "LoadAware"

	DefaultNodeMonitorReportInterval       = 60 * time.Second
	DefaultMilliCPURequest           int64 = 250               // 0.25 core
	DefaultMemoryRequest             int64 = 200 * 1024 * 1024 // 200 MB

	portraitItemsLength = 24
	portraitNameFmt     = "auto-created-%s-%s-%s-0" // auto-created-{appName}-{workloadType}-{workloadName}-index
	portraitAppName     = "overcommit"

	cpuUsageMetric    = "cpu_utilization_usage_seconds_max"
	memoryUsageMetric = "memory_utilization_max"
)

var (
	_ framework.FilterPlugin  = &Plugin{}
	_ framework.ScorePlugin   = &Plugin{}
	_ framework.ReservePlugin = &Plugin{}
)

type Plugin struct {
	handle            framework.Handle
	args              *config.LoadAwareArgs
	nodeMonitorLister listers.NodeMonitorLister
	portraitLister    portraitlisters.PortraitLister
	portraitHasSynced toolscache.InformerSynced
}

func NewPlugin(args runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	klog.Infof("new loadAware scheduler plugin")
	pluginArgs, ok := args.(*config.LoadAwareArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type LoadAwareArgs, got %T", args)
	}
	if err := validation.ValidateLoadAwareSchedulingArgs(pluginArgs); err != nil {
		klog.Errorf("validate pluginArgs fail, err: %v", err)
		return nil, err
	}

	p := &Plugin{
		handle: handle,
		args:   pluginArgs,
	}
	p.registerNodeMonitorHandler()
	p.registerPortraitHandler()
	RegisterPodHandler()

	if pluginArgs.EnablePortrait {
		cache.SetPortraitLister(p)
	}

	go func() {
		wait.Until(func() {
			if p.portraitHasSynced == nil || !p.portraitHasSynced() {
				klog.Warningf("portrait has not synced, skip")
				return
			}
			cache.ReconcilePredictUsage()
		}, time.Hour, context.TODO().Done())
	}()

	return p, nil
}

func (p *Plugin) Name() string {
	return Name
}

func (p *Plugin) GetPodPortrait(pod *v1.Pod) *ResourceUsage {
	ownerName, ownerKind, ok := podToWorkloadByOwner(pod)
	klog.V(6).Infof("pod %v ownerName: %v, ownerKind: %v", pod.Name, ownerName, ownerKind)
	if !ok {
		// return result by pod request
		return p.portraitByRequest(pod)
	}

	// get portrait
	portrait, err := p.getPortrait(pod, ownerName, ownerKind)
	if err != nil {
		return p.portraitByRequest(pod)
	}

	// validate and transfer portrait
	podResourceUsage := &ResourceUsage{}

	cpuUsage, err := p.portraitToTimeSeries(portrait, cpuUsageMetric)
	if err != nil {
		klog.Errorf("pod %v metric: %v portraitToTimeSeries err: %v", pod.Name, cpuUsageMetric, err)
		resourceList := native.SumUpPodRequestResources(pod)
		cpuScaleFactor := p.args.ResourceToScalingFactorMap[v1.ResourceCPU]
		if cpuScaleFactor == 0 {
			cpuScaleFactor = 100
		}
		cpuUsage = cpuTimeSeriesByRequest(resourceList, float64(cpuScaleFactor)/100.0)
	}

	memoryUsage, err := p.portraitToTimeSeries(portrait, memoryUsageMetric)
	if err != nil {
		klog.Errorf("pod %v metric: %v portraitToTimeSeries err: %v", pod.Name, memoryUsageMetric, err)
		resourceList := native.SumUpPodRequestResources(pod)
		memScaleFactor := p.args.ResourceToScalingFactorMap[v1.ResourceMemory]
		if memScaleFactor == 0 {
			memScaleFactor = 100
		}
		memoryUsage = memoryTimeSeriesByRequest(resourceList, float64(memScaleFactor)/100.0)
	}

	podResourceUsage.Cpu = cpuUsage
	podResourceUsage.Memory = memoryUsage

	return podResourceUsage
}

func (p *Plugin) portraitToTimeSeries(portrait *v1alpha12.Portrait, metricName string) ([]float64, error) {
	if portrait == nil {
		return nil, fmt.Errorf("portrait is nil")
	}

	items, ok := portrait.TimeSeries[metricName]
	if !ok {
		return nil, fmt.Errorf("metric %v not found in portrait", metricName)
	}

	if len(items) != portraitItemsLength {
		return nil, fmt.Errorf("metric %v portrait length %v is not support", metricName, len(items))
	}

	itemsCopy := make([]v1alpha12.Item, 0)
	for _, item := range items {
		value := item.Value
		if metricName == cpuUsageMetric {
			value *= 1000
		}
		itemsCopy = append(itemsCopy, v1alpha12.Item{
			Timestamp: item.Timestamp,
			Value:     value,
		})
	}

	sort.Sort(Items(itemsCopy))

	res := make([]float64, portraitItemsLength, portraitItemsLength)
	for i := range itemsCopy {
		res[i] = itemsCopy[i].Value
	}
	return res, nil
}

func (p *Plugin) portraitByRequest(pod *v1.Pod) *ResourceUsage {
	res := &ResourceUsage{}

	resourceList := native.SumUpPodRequestResources(pod)

	cpuScaleFactor := p.args.ResourceToScalingFactorMap[v1.ResourceCPU]
	if cpuScaleFactor == 0 {
		cpuScaleFactor = 100
	}
	memScaleFactor := p.args.ResourceToScalingFactorMap[v1.ResourceMemory]
	if memScaleFactor == 0 {
		memScaleFactor = 100
	}
	cpuSeries := cpuTimeSeriesByRequest(resourceList, float64(cpuScaleFactor)/100.0)
	memSeries := memoryTimeSeriesByRequest(resourceList, float64(memScaleFactor)/100.0)

	res.Cpu = cpuSeries
	res.Memory = memSeries
	return res
}

func (p *Plugin) getPortrait(pod *v1.Pod, workloadName, workloadType string) (*v1alpha12.Portrait, error) {
	portraitName := resourcePortraitResultName(workloadName, workloadType, portraitNameFmt, portraitAppName)

	rpResult, err := p.portraitLister.Portraits(pod.GetNamespace()).Get(portraitName)
	if err != nil {
		klog.V(5).Infof("get portrait %v namespace %v fail: %v", portraitName, pod.GetNamespace(), err)
		return nil, err
	}
	return rpResult, nil
}

func (p *Plugin) getNodePredictUsage(pod *v1.Pod, nodeName string) (*ResourceUsage, error) {
	nodePredictUsage := cache.GetNodePredictUsage(nodeName)
	klog.V(6).Infof("node %v predict usage cpu: %v, memory: %v", nodeName, nodePredictUsage.Cpu, nodePredictUsage.Memory)

	podPredictUsage := p.GetPodPortrait(pod)
	klog.V(6).Infof("pod %v predict usage cpu: %v, memory: %v", pod.Name, podPredictUsage.Cpu, podPredictUsage.Memory)

	err := nodePredictUsage.add(podPredictUsage)
	if err != nil {
		err = fmt.Errorf("sum node %s predict usage fail: %v", nodeName, err)
		return nil, err
	}

	return nodePredictUsage, nil
}

func (p *Plugin) IsLoadAareEnabled(pod *v1.Pod) bool {
	if p.args.PodAnnotationLoadAwareEnable == nil || *p.args.PodAnnotationLoadAwareEnable == "" {
		return true
	}

	if flag, ok := pod.Annotations[*p.args.PodAnnotationLoadAwareEnable]; ok && flag == consts.PodAnnotationLoadAwareEnableTrue {
		return true
	}
	return false
}

func isNodeMonitorExpired(nodeMonitor *v1alpha1.NodeMonitor, nodeMetricExpirationSeconds int64) bool {
	return nodeMonitor.Status.UpdateTime == nil || nodeMetricExpirationSeconds > 0 &&
		time.Since(nodeMonitor.Status.UpdateTime.Time) > time.Duration(nodeMetricExpirationSeconds)*time.Second
}
