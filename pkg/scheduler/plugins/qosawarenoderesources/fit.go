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

package qosawarenoderesources

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"

	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config"
	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config/validation"
	"github.com/kubewharf/katalyst-api/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/scheduler/cache"
	"github.com/kubewharf/katalyst-core/pkg/scheduler/eventhandlers"
	"github.com/kubewharf/katalyst-core/pkg/scheduler/util"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

var _ framework.PreFilterPlugin = &Fit{}
var _ framework.FilterPlugin = &Fit{}
var _ framework.ScorePlugin = &Fit{}
var _ framework.ReservePlugin = &Fit{}

const (
	// FitName is the name of the plugin used in the plugin registry and configurations.
	FitName = "QoSAwareNodeResourcesFit"

	// preFilterStateKey is the key in CycleState to NodeResourcesFit pre-computed data.
	// Using the name of the plugin will likely help us avoid collisions with other plugins.
	preFilterStateKey = "PreFilter" + FitName
)

// nodeResourceStrategyTypeMap maps strategy to scorer implementation
var nodeResourceStrategyTypeMap = map[config.ScoringStrategyType]scorer{
	config.LeastAllocated: func(args *config.QoSAwareNodeResourcesFitArgs) *resourceAllocationScorer {
		resToWeightMap := resourcesToWeightMap(args.ScoringStrategy.ReclaimedResources)
		return &resourceAllocationScorer{
			Name:                string(config.LeastAllocated),
			scorer:              leastResourceScorer(resToWeightMap),
			resourceToWeightMap: resToWeightMap,
		}
	},
	config.MostAllocated: func(args *config.QoSAwareNodeResourcesFitArgs) *resourceAllocationScorer {
		resToWeightMap := resourcesToWeightMap(args.ScoringStrategy.ReclaimedResources)
		return &resourceAllocationScorer{
			Name:                string(config.MostAllocated),
			scorer:              mostResourceScorer(resToWeightMap),
			resourceToWeightMap: resToWeightMap,
		}
	},
	config.RequestedToCapacityRatio: func(args *config.QoSAwareNodeResourcesFitArgs) *resourceAllocationScorer {
		resToWeightMap := resourcesToWeightMap(args.ScoringStrategy.ReclaimedResources)
		return &resourceAllocationScorer{
			Name:                string(config.RequestedToCapacityRatio),
			scorer:              requestedToCapacityRatioScorer(resToWeightMap, args.ScoringStrategy.ReclaimedRequestedToCapacityRatio.Shape),
			resourceToWeightMap: resToWeightMap,
		}
	},
}

// Fit is a plugin that checks if a node has sufficient resources.
type Fit struct {
	handle framework.Handle
	resourceAllocationScorer
	nativeScore framework.ScorePlugin
}

// ScoreExtensions of the Score plugin.
func (f *Fit) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// preFilterState computed at PreFilter and used at Filter.
type preFilterState struct {
	native.QoSResource
}

// Clone the prefilter state.
func (s *preFilterState) Clone() framework.StateData {
	return s
}

// Name returns name of the plugin. It is used in logs, etc.
func (f *Fit) Name() string {
	return FitName
}

// NewFit initializes a new plugin and returns it.
func NewFit(plArgs runtime.Object, h framework.Handle) (framework.Plugin, error) {
	args, ok := plArgs.(*config.QoSAwareNodeResourcesFitArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type NodeQoSResourcesFitArgs, got %T", plArgs)
	}
	if err := validation.ValidateQoSAwareNodeResourcesFitArgs(nil, args); err != nil {
		return nil, err
	}

	if args.ScoringStrategy == nil {
		return nil, fmt.Errorf("scoring strategy not specified")
	}

	strategy := args.ScoringStrategy.Type
	scorePlugin, exists := nodeResourceStrategyTypeMap[strategy]
	if !exists {
		return nil, fmt.Errorf("scoring strategy %s is not supported", strategy)
	}

	var (
		nativeScore framework.ScorePlugin
		err         error
	)
	switch args.ScoringStrategy.Type {
	case config.LeastAllocated:
		nativeScore, err = newLeastAllocated(args, h)
	case config.MostAllocated:
		nativeScore, err = newMostAllocated(args, h)
	case config.RequestedToCapacityRatio:
		nativeScore, err = newRequestedToCapacityRatio(args, h)
	}
	if err != nil {
		return nil, err
	}

	eventhandlers.RegisterCommonPodHandler()
	eventhandlers.RegisterCommonCNRHandler()

	return &Fit{
		handle:                   h,
		resourceAllocationScorer: *scorePlugin(args),
		nativeScore:              nativeScore,
	}, nil
}

func newLeastAllocated(args *config.QoSAwareNodeResourcesFitArgs, h framework.Handle) (framework.ScorePlugin, error) {
	leastAllocatedArgs := &kubeschedulerconfig.NodeResourcesLeastAllocatedArgs{
		Resources: args.ScoringStrategy.Resources,
	}

	leastAllocatedPlugin, err := noderesources.NewLeastAllocated(leastAllocatedArgs, h)
	if err != nil {
		return nil, err
	}

	leastAllocated, ok := leastAllocatedPlugin.(*noderesources.LeastAllocated)
	if !ok {
		return nil, fmt.Errorf("newLeastAllocated type error")
	}

	return leastAllocated, nil
}

func newMostAllocated(args *config.QoSAwareNodeResourcesFitArgs, h framework.Handle) (framework.ScorePlugin, error) {
	mostAllocatedArgs := &kubeschedulerconfig.NodeResourcesMostAllocatedArgs{
		Resources: args.ScoringStrategy.Resources,
	}

	mostAllocatedPlugin, err := noderesources.NewMostAllocated(mostAllocatedArgs, h)
	if err != nil {
		return nil, err
	}

	mostAllocated, ok := mostAllocatedPlugin.(*noderesources.MostAllocated)
	if !ok {
		return nil, fmt.Errorf("newMostAllocated type error")
	}

	return mostAllocated, nil
}

func newRequestedToCapacityRatio(args *config.QoSAwareNodeResourcesFitArgs, h framework.Handle) (framework.ScorePlugin, error) {
	requestedToCapacityRatioArgs := args.ScoringStrategy.RequestedToCapacityRatio

	requestedToCapacityRatioPlugin, err := noderesources.NewRequestedToCapacityRatio(requestedToCapacityRatioArgs, h)
	if err != nil {
		return nil, err
	}

	requestedToCapacityRatio, ok := requestedToCapacityRatioPlugin.(*noderesources.RequestedToCapacityRatio)
	if !ok {
		return nil, fmt.Errorf("newMostAllocated type error")
	}

	return requestedToCapacityRatio, nil
}

// PreFilter invoked at the prefilter extension point.
func (f *Fit) PreFilter(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod) *framework.Status {
	if !util.IsReclaimedPod(pod) {
		return nil
	}
	cycleState.Write(preFilterStateKey, computePodQoSResourceRequest(pod))
	return nil
}

// PreFilterExtensions returns prefilter extensions, pod add and remove.
func (f *Fit) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// computePodQoSResourceRequest returns a framework.Resource that covers the largest
// width in each resource dimension. Because init-containers run sequentially, we collect
// the max in each dimension iteratively. In contrast, we sum the resource vectors for
// regular containers since they run simultaneously.
//
// the resources defined for Overhead should be added to the calculated QoSResource request sum
//
// example:
/*
// Pod:
//   InitContainers
//     IC1:
//       CPU: 2
//       Memory: 1G
//     IC2:
//       CPU: 2
//       Memory: 3G
//   Containers
//     C1:
//       CPU: 2
//       Memory: 1G
//     C2:
//       CPU: 1
//       Memory: 1G
//
// Result: CPU: 3, Memory: 3G
*/
func computePodQoSResourceRequest(pod *v1.Pod) *preFilterState {
	result := &preFilterState{}
	for _, container := range pod.Spec.Containers {
		result.Add(container.Resources.Requests)
	}

	// take max_resource(sum_pod, any_init_container)
	for _, container := range pod.Spec.InitContainers {
		result.SetMaxResource(container.Resources.Requests)
	}

	// If Overhead is being utilized, add to the total requests for the pod
	if pod.Spec.Overhead != nil {
		result.Add(pod.Spec.Overhead)
	}
	return result
}

func getPreFilterState(cycleState *framework.CycleState) (*preFilterState, error) {
	c, err := cycleState.Read(preFilterStateKey)
	if err != nil {
		// preFilterState doesn't exist, likely PreFilter wasn't invoked.
		return nil, fmt.Errorf("error reading %q from cycleState: %w", preFilterStateKey, err)
	}

	s, ok := c.(*preFilterState)
	if !ok {
		return nil, fmt.Errorf("%+v  convert to NodeQoSResourcesFit.preFilterState error", c)
	}
	return s, nil
}

// Filter invoked at the filter extension point.
// Checks if a node has sufficient resources, such as cpu, memory, gpu, opaque int resources etc to run a pod.
// It returns a list of insufficient resources, if empty, then the node has all the resources requested by the pod.
func (f *Fit) Filter(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if !util.IsReclaimedPod(pod) {
		return nil
	}

	s, err := getPreFilterState(cycleState)
	if err != nil {
		return framework.AsStatus(err)
	}

	insufficientResources := fitsRequest(s, nodeInfo)

	if len(insufficientResources) != 0 {
		// We will keep all failure reasons.
		failureReasons := make([]string, 0, len(insufficientResources))
		for i := range insufficientResources {
			failureReasons = append(failureReasons, insufficientResources[i].Reason)
		}
		return framework.NewStatus(framework.Unschedulable, failureReasons...)
	}

	return nil
}

// InsufficientResource describes what kind of resource limit is hit and caused the pod to not fit the node.
type InsufficientResource struct {
	ResourceName v1.ResourceName
	// We explicitly have a parameter for reason to avoid formatting a message on the fly
	// for common resources, which is expensive for cluster autoscaler simulations.
	Reason    string
	Requested int64
	Used      int64
	Capacity  int64
}

func fitsRequest(podRequest *preFilterState, nodeInfo *framework.NodeInfo) []InsufficientResource {
	insufficientResources := make([]InsufficientResource, 0, 2)

	if podRequest.ReclaimedMilliCPU == 0 &&
		podRequest.ReclaimedMemory == 0 {
		return insufficientResources
	}

	extendedNodeInfo, err := cache.GetCache().GetNodeInfo(nodeInfo.Node().GetName())
	if err != nil {
		insufficientResources = append(insufficientResources,
			InsufficientResource{
				Reason: err.Error(),
			},
		)
		return insufficientResources
	}

	extendedNodeInfo.Mutex.RLock()
	defer extendedNodeInfo.Mutex.RUnlock()

	if podRequest.ReclaimedMilliCPU > (extendedNodeInfo.QoSResourcesAllocatable.ReclaimedMilliCPU - extendedNodeInfo.QoSResourcesRequested.ReclaimedMilliCPU) {
		insufficientResources = append(insufficientResources, InsufficientResource{
			ResourceName: consts.ReclaimedResourceMilliCPU,
			Reason:       fmt.Sprintf("Insufficient %s", consts.ReclaimedResourceMilliCPU),
			Requested:    podRequest.ReclaimedMilliCPU,
			Used:         extendedNodeInfo.QoSResourcesRequested.ReclaimedMilliCPU,
			Capacity:     extendedNodeInfo.QoSResourcesAllocatable.ReclaimedMilliCPU,
		})
	}
	if podRequest.ReclaimedMemory > (extendedNodeInfo.QoSResourcesAllocatable.ReclaimedMemory - extendedNodeInfo.QoSResourcesRequested.ReclaimedMemory) {
		insufficientResources = append(insufficientResources, InsufficientResource{
			ResourceName: consts.ReclaimedResourceMemory,
			Reason:       fmt.Sprintf("Insufficient %s", consts.ReclaimedResourceMemory),
			Requested:    podRequest.ReclaimedMemory,
			Used:         extendedNodeInfo.QoSResourcesRequested.ReclaimedMemory,
			Capacity:     extendedNodeInfo.QoSResourcesAllocatable.ReclaimedMemory,
		})
	}

	return insufficientResources
}

// Score invoked at the Score extension point.
func (f *Fit) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	if util.IsReclaimedPod(pod) {
		extendedNodeInfo, err := cache.GetCache().GetNodeInfo(nodeName)
		if err != nil {
			return 0, framework.AsStatus(fmt.Errorf("getting node %q error: %w", nodeName, err))
		}

		return f.score(pod, extendedNodeInfo, nodeName)
	}

	return f.nativeScore.Score(ctx, state, pod, nodeName)
}

// Reserve is the functions invoked by the framework at "Reserve" extension point.
func (f *Fit) Reserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	if !util.IsReclaimedPod(pod) || nodeName == "" || native.PodIsTerminated(pod) {
		return nil
	}

	newPod := pod.DeepCopy()
	newPod.Spec.NodeName = nodeName

	if err := cache.GetCache().AddPod(newPod); err != nil {
		return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("extended cache reserve failed, err: %s", err.Error()))
	}

	return nil
}

// Unreserve is the functions invoked by the framework at "Unreserve" extension point.
func (f *Fit) Unreserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	if !util.IsReclaimedPod(pod) || nodeName == "" {
		return
	}

	newPod := pod.DeepCopy()
	newPod.Spec.NodeName = nodeName

	if err := cache.GetCache().RemovePod(newPod); err != nil {
		klog.ErrorS(err, "Unreserve failed to RemovePod",
			"pod", klog.KObj(pod), "node", nodeName)
	}
}
