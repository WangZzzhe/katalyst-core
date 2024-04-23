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
	if p.IsLoadAareEnabled(pod) {
		return nil
	}

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
		if usage > threshold {
			return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("node(s) %s usage exceed threshold, usage:%v, threshold: %v ", resourceName, usage, threshold))
		}
	}

	return nil
}
