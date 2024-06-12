package loadaware

import (
	"context"
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kubewharf/katalyst-api/pkg/consts"
)

func (p *Plugin) Filter(_ context.Context, _ *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if !p.IsLoadAareEnabled(pod) {
		return nil
	}

	status := p.fitByNodeMonitor(nodeInfo)
	if status != nil || status.IsUnschedulable() {
		return status
	}

	if p.args.EnablePortrait {
		status = p.fitByPortrait(pod, nodeInfo)
		return status
	}
	klog.V(6).Infof("loadaware portrait unable, skip")
	return status
}

func (p *Plugin) fitByNodeMonitor(nodeInfo *framework.NodeInfo) *framework.Status {
	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Unschedulable, "node not found")
	}
	if len(p.args.ResourceToThresholdMap) == 0 {
		klog.Warningf("load aware fit missing required args")
		return nil
	}

	// get nodeMonitor from informer
	nodeMonitor, err := p.nodeMonitorLister.Get(node.Name)
	if err != nil {
		klog.Errorf("get NodeMonitor of node %v failed, err: %v", node.Name, err)
		return nil
	}
	if nodeMonitor.Status.NodeUsage == nil {
		klog.Errorf("NodeMonitor of node %s status NodeUsage is nil", node.Name)
		return nil
	}

	// check if nodeMonitor data expired
	if p.args.FilterExpiredNodeMonitor != nil && *p.args.FilterExpiredNodeMonitor && p.args.NodeMonitorExpiredSeconds != nil {
		if isNodeMonitorExpired(nodeMonitor, *p.args.NodeMonitorExpiredSeconds) {
			klog.Warningf("NodeMonitor of node %s is expired", node.Name)
			return nil
		}
	}

	usageInfo := nodeMonitor.Status.NodeUsage[consts.Usage15MinAvgKey]
	if usageInfo == nil {
		klog.Errorf("NodeMonitor of node %s status NodeUsage miss avg_15min metrics", node.Name)
		return nil
	}
	for resourceName, threshold := range p.args.ResourceToThresholdMap {
		if threshold == 0 {
			continue
		}
		total := node.Status.Allocatable[resourceName]
		if total.IsZero() {
			continue
		}
		used := usageInfo[resourceName]
		usage := int64(math.Round(float64(used.MilliValue()) / float64(total.MilliValue()) * 100))
		klog.V(6).Infof("node %v usage %v, threshold %v", nodeInfo.Node().Name, usage, threshold)
		if usage > threshold {
			return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("node(s) %s usage exceed threshold, usage:%v, threshold: %v ", resourceName, usage, threshold))
		}
	}

	return nil
}

func (p *Plugin) fitByPortrait(pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if pod == nil {
		return nil
	}
	if nodeInfo == nil || nodeInfo.Node() == nil {
		return nil
	}

	nodePredictUsage, err := p.getNodePredictUsage(pod, nodeInfo.Node().Name)
	if err != nil {
		klog.Error(err)
		return nil
	}

	// check if nodePredictUsage is greater than threshold
	for _, resourceName := range []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory} {
		threshold, ok := p.args.ResourceToThresholdMap[resourceName]
		if !ok {
			continue
		}
		total := nodeInfo.Node().Status.Allocatable[resourceName]
		if total.IsZero() {
			continue
		}
		var totalValue int64
		if resourceName == v1.ResourceCPU {
			totalValue = total.MilliValue()
		} else {
			totalValue = total.Value()
		}

		maxUsage := nodePredictUsage.max(resourceName)
		usageRatio := int64(math.Round(maxUsage / float64(totalValue) * 100))
		klog.V(6).Infof("loadAware fit pod %v, node %v, resource %v, threshold: %v, usageRatio: %v, maxUsage: %v, nodeTotal %v",
			pod.Name, nodeInfo.Node().Name, resourceName, threshold, usageRatio, maxUsage, totalValue)

		if usageRatio > threshold {
			return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("node(s) %s usage exceed threshold, usage:%v, threshold: %v ", resourceName, usageRatio, threshold))
		}
	}

	return nil
}
