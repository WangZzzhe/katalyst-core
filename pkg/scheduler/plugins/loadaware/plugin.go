package loadaware

import (
	"time"

	"github.com/kubewharf/katalyst-api/pkg/consts"

	"github.com/kubewharf/katalyst-api/pkg/apis/node/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config"
	listers "github.com/kubewharf/katalyst-api/pkg/client/listers/node/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	Name = "LoadAware"
)

var (
	_ framework.FilterPlugin  = &Plugin{}
	_ framework.ScorePlugin   = &Plugin{}
	_ framework.ReservePlugin = &Plugin{}
)

type Plugin struct {
	handler           framework.Handle
	args              *config.LoadAwareArgs
	nodeMonitorLister listers.NodeMonitorLister
}

func (p *Plugin) Name() string {
	return Name
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
