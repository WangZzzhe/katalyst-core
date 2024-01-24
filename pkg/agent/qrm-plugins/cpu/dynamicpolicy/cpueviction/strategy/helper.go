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

package strategy

import (
	"github.com/kubewharf/katalyst-core/pkg/config"
	"github.com/kubewharf/katalyst-core/pkg/metaserver"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
	v1 "k8s.io/api/core/v1"
)

// eviction scope related variables
// actionReclaimedEviction for reclaimed_cores, while actionEviction for all pods
const (
	actionNoop = iota
	actionSuppression
	actionEviction
)

// EvictionHelper is a general tool collection for all memory eviction plugin
type EvictionHelper struct {
	metaServer         *metaserver.MetaServer
	emitter            metrics.MetricEmitter
	reclaimedPodFilter func(pod *v1.Pod) (bool, error)
}

func NewEvictionHelper(emitter metrics.MetricEmitter, metaServer *metaserver.MetaServer, conf *config.Configuration) *EvictionHelper {
	return &EvictionHelper{
		metaServer:         metaServer,
		emitter:            emitter,
		reclaimedPodFilter: conf.CheckReclaimedQoSForPod,
	}
}

func (e *EvictionHelper) filterPods(pods []*v1.Pod, action int) []*v1.Pod {
	switch action {
	case actionEviction:
		return native.FilterPods(pods, e.reclaimedPodFilter)
	//case actionSuppression:
	//	return pods
	default:
		return nil
	}
}
