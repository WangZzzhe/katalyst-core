package loadaware

import (
	"context"
	"fmt"
	"math"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	resourceapi "k8s.io/kubernetes/pkg/api/v1/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kubewharf/katalyst-api/pkg/apis/node/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config"
	"github.com/kubewharf/katalyst-api/pkg/consts"
)

func (p *Plugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func (p *Plugin) Score(_ context.Context, _ *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	if !p.IsLoadAareEnabled(pod) {
		return 0, nil
	}

	nodeInfo, err := p.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Unschedulable, fmt.Sprintf("get node %v from Snapshot: %v", nodeName, err))
	}
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Unschedulable, "node not found")
	}
	nodeMonitor, err := p.nodeMonitorLister.Get(nodeName)
	if err != nil {
		return 0, nil
	}
	if p.args.NodeMonitorExpiredSeconds != nil && isNodeMonitorExpired(nodeMonitor, *p.args.NodeMonitorExpiredSeconds) {
		return 0, nil
	}

	//estimated the recent assign pod usage
	estimatedUsed := estimatedPodUsed(pod, p.args.ResourceToWeightMap, p.args.ResourceToScalingFactorMap)
	estimatedAssignedPodUsage := p.estimatedAssignedPodUsage(nodeName, nodeMonitor)
	finalEstimatedUsed := quotav1.Add(estimatedUsed, estimatedAssignedPodUsage)
	//add estimated usage to avg_15min_usage
	finalNodeUsedOfIndicators := make(map[config.IndicatorType]v1.ResourceList)
	for indicator := range p.args.CalculateIndicatorWeight {
		if nodeMonitor.Status.NodeUsage != nil {
			used := nodeMonitor.Status.NodeUsage[string(indicator)]
			if indicator == consts.Usage15MinAvgKey {
				used = quotav1.Add(used, finalEstimatedUsed)
			}
			finalNodeUsedOfIndicators[indicator] = used
		}
	}
	score := loadAwareSchedulingScorer(finalNodeUsedOfIndicators, node.Status.Allocatable, p.args.ResourceToWeightMap, p.args.CalculateIndicatorWeight)
	return score, nil
}

func (p *Plugin) estimatedAssignedPodUsage(nodeName string, nodeMonitor *v1alpha1.NodeMonitor) v1.ResourceList {
	cache.RLock()
	defer cache.RUnlock()
	var (
		estimatedUsed = make(map[v1.ResourceName]int64)
		result        = v1.ResourceList{}
	)
	nodeCache, ok := cache.NodePodInfo[nodeName]
	if !ok {
		return result
	}

	for _, podInfo := range nodeCache.PodInfoMap {
		if isNeedToEstimatedUsage(podInfo, nodeMonitor) {
			estimated := estimatedPodUsed(podInfo.pod, p.args.ResourceToWeightMap, p.args.ResourceToScalingFactorMap)
			for resourceName, quantity := range estimated {
				if resourceName == v1.ResourceCPU {
					estimatedUsed[resourceName] += quantity.MilliValue()
				} else {
					estimatedUsed[resourceName] += quantity.Value()
				}
			}
		}
	}
	// transfer map[ResourceName]int64 to ResourceList
	for resourceName, value := range estimatedUsed {
		if resourceName == v1.ResourceCPU {
			result[resourceName] = *resource.NewMilliQuantity(value, resource.DecimalSI)
		} else {
			result[resourceName] = *resource.NewQuantity(value, resource.DecimalSI)
		}
	}
	return result
}

func estimatedPodUsed(pod *v1.Pod, resourceWeights map[v1.ResourceName]int64, scalingFactors map[v1.ResourceName]int64) v1.ResourceList {
	requests, limits := resourceapi.PodRequestsAndLimits(pod)
	estimatedUsed := v1.ResourceList{}
	for resourceName := range resourceWeights {
		value := estimatedUsedByResource(requests, limits, resourceName, scalingFactors[resourceName])
		if resourceName == v1.ResourceCPU {
			estimatedUsed[resourceName] = *resource.NewMilliQuantity(value, resource.DecimalSI)
		} else {
			estimatedUsed[resourceName] = *resource.NewQuantity(value, resource.DecimalSI)
		}
	}
	return estimatedUsed
}

func isNeedToEstimatedUsage(podInfo *PodInfo, nodeMonitor *v1alpha1.NodeMonitor) bool {
	nodeMonitorReportInterval := getNodeMonitorReportInterval(nodeMonitor)
	return podInfo.startTime.After(nodeMonitor.Status.UpdateTime.Time) ||
		podInfo.startTime.Before(nodeMonitor.Status.UpdateTime.Time) &&
			nodeMonitor.Status.UpdateTime.Sub(podInfo.startTime) < nodeMonitorReportInterval
}

func getNodeMonitorReportInterval(nodeMonitor *v1alpha1.NodeMonitor) time.Duration {
	if nodeMonitor.Spec.ReportInterval == nil {
		return DefaultNodeMonitorReportInterval
	}
	return nodeMonitor.Spec.ReportInterval.Duration
}

func estimatedUsedByResource(requests, limits v1.ResourceList, resourceName v1.ResourceName, scalingFactor int64) int64 {
	limitQuantity := limits[resourceName]
	requestQuantity := requests[resourceName]
	var quantity resource.Quantity
	if limitQuantity.Cmp(requestQuantity) > 0 {
		scalingFactor = 100
		quantity = limitQuantity
	} else {
		quantity = requestQuantity
	}

	if quantity.IsZero() {
		switch resourceName {
		case v1.ResourceCPU:
			return DefaultMilliCPURequest
		case v1.ResourceMemory:
			return DefaultMemoryRequest
		}
		return 0
	}

	var estimatedUsed int64
	switch resourceName {
	case v1.ResourceCPU:
		estimatedUsed = int64(math.Round(float64(quantity.MilliValue()) * float64(scalingFactor) / 100))
	default:
		estimatedUsed = int64(math.Round(float64(quantity.Value()) * float64(scalingFactor) / 100))
	}
	return estimatedUsed
}

// first calculate cpu/memory score according to avg_15min, max_1hour, max_1day  and its weight
// then calculate final score with cpuScore and memoryScore with its weight
func loadAwareSchedulingScorer(usedOfIndicators map[config.IndicatorType]v1.ResourceList, allocatable v1.ResourceList, resourceWeight map[v1.ResourceName]int64, indicatorRatio map[config.IndicatorType]int64) int64 {
	var nodeScore, weightSum int64
	// cpu and memory weight
	for resourceName, weight := range resourceWeight {
		resourceSumScore := int64(0)
		ratioSum := int64(0)
		// calculate cpu/memory score by avg_15min, max_1hour, max_1day
		for indicatorName, ratio := range indicatorRatio {
			alloc, ok := allocatable[resourceName]
			if !ok {
				continue
			}
			resList := usedOfIndicators[indicatorName]
			if resList == nil {
				continue
			}
			quantity, ok := resList[resourceName]
			if !ok {
				continue
			}
			resourceScore := int64(0)
			if resourceName == v1.ResourceCPU {
				resourceScore = leastUsedScore(quantity.MilliValue(), alloc.MilliValue())
			} else {
				resourceScore = leastUsedScore(quantity.Value(), alloc.Value())
			}
			resourceSumScore += resourceScore * ratio
			ratioSum += ratio
		}
		nodeScore += (resourceSumScore / ratioSum) * weight
		weightSum += weight
	}

	return nodeScore / weightSum
}

func leastUsedScore(used, capacity int64) int64 {
	if capacity == 0 {
		return 0
	}
	if used > capacity {
		return 0
	}
	return ((capacity - used) * framework.MaxNodeScore) / capacity
}
