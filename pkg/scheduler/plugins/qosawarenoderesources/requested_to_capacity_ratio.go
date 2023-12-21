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
	"math"

	"k8s.io/kubernetes/pkg/scheduler/algorithm/priorities"

	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
)

const (
	maxUtilization = 100
)

// buildRequestedToCapacityRatioScorerFunction allows users to apply bin packing
// on core resources like CPU, Memory as well as extended resources like accelerators.
func buildRequestedToCapacityRatioScorerFunction(scoringFunctionShape priorities.FunctionShape, resourceToWeightMap resourceToWeightMap) func(resourceToValueMap, resourceToValueMap) int64 {
	rawScoringFunction := buildBrokenLinearFunction(scoringFunctionShape)
	resourceScoringFunction := func(requested, capacity int64) int64 {
		if capacity == 0 || requested > capacity {
			return rawScoringFunction(maxUtilization)
		}

		return rawScoringFunction(requested * maxUtilization / capacity)
	}
	return func(requested, allocatable resourceToValueMap) int64 {
		var nodeScore, weightSum int64
		for resource := range requested {
			weight := resourceToWeightMap[resource]
			resourceScore := resourceScoringFunction(requested[resource], allocatable[resource])
			if resourceScore > 0 {
				nodeScore += resourceScore * weight
				weightSum += weight
			}
		}
		if weightSum == 0 {
			return 0
		}
		return int64(math.Round(float64(nodeScore) / float64(weightSum)))
	}
}

func requestedToCapacityRatioScorer(weightMap resourceToWeightMap, shape []kubeschedulerconfig.UtilizationShapePoint) func(resourceToValueMap, resourceToValueMap) int64 {
	shapes := make([]priorities.FunctionShapePoint, 0, len(shape))
	for _, point := range shape {
		shapes = append(shapes, priorities.FunctionShapePoint{
			Utilization: int64(point.Utilization),
			// MaxCustomPriorityScore may diverge from the max score used in the scheduler and defined by MaxNodeScore,
			// therefore we need to scale the score returned by requested to capacity ratio to the score range
			// used by the scheduler.
			Score: int64(point.Score) * (framework.MaxNodeScore / kubeschedulerconfig.MaxCustomPriorityScore),
		})
	}

	return buildRequestedToCapacityRatioScorerFunction(shapes, weightMap)
}

// Creates a function which is built using linear segments. Segments are defined via shape array.
// Shape[i].Utilization slice represents points on "utilization" axis where different segments meet.
// Shape[i].Score represents function values at meeting points.
//
// function f(p) is defined as:
//
//	shape[0].Score for p < f[0].Utilization
//	shape[i].Score for p == shape[i].Utilization
//	shape[n-1].Score for p > shape[n-1].Utilization
//
// and linear between points (p < shape[i].Utilization)
func buildBrokenLinearFunction(shape priorities.FunctionShape) func(int64) int64 {
	n := len(shape)
	return func(p int64) int64 {
		for i := 0; i < n; i++ {
			if p <= shape[i].Utilization {
				if i == 0 {
					return shape[0].Score
				}
				return shape[i-1].Score + (shape[i].Score-shape[i-1].Score)*(p-shape[i-1].Utilization)/(shape[i].Utilization-shape[i-1].Utilization)
			}
		}
		return shape[n-1].Score
	}
}
