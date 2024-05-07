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

	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	maxUtilization = 100
)

type functionShape []functionShapePoint

type functionShapePoint struct {
	// Utilization is function argument.
	utilization int64
	// Score is function value.
	score int64
}

// buildRequestedToCapacityRatioScorerFunction allows users to apply bin packing
// on core resources like CPU, Memory as well as extended resources like accelerators.
func buildRequestedToCapacityRatioScorerFunction(scoringFunctionShape functionShape, resourceToWeightMap resourceToWeightMap) func(resourceToValueMap, resourceToValueMap) int64 {
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
	shapes := make([]functionShapePoint, 0, len(shape))
	for _, point := range shape {
		shapes = append(shapes, functionShapePoint{
			utilization: int64(point.Utilization),
			// MaxCustomPriorityScore may diverge from the max score used in the scheduler and defined by MaxNodeScore,
			// therefore we need to scale the score returned by requested to capacity ratio to the score range
			// used by the scheduler.
			score: int64(point.Score) * (framework.MaxNodeScore / kubeschedulerconfig.MaxCustomPriorityScore),
		})
	}

	return buildRequestedToCapacityRatioScorerFunction(shapes, weightMap)
}

// Creates a function which is built using linear segments. Segments are defined via shape array.
// Shape[i].utilization slice represents points on "utilization" axis where different segments meet.
// Shape[i].score represents function values at meeting points.
//
// function f(p) is defined as:
//
//	shape[0].score for p < f[0].utilization
//	shape[i].score for p == shape[i].utilization
//	shape[n-1].score for p > shape[n-1].utilization
//
// and linear between points (p < shape[i].utilization)
func buildBrokenLinearFunction(shape functionShape) func(int64) int64 {
	return func(p int64) int64 {
		for i := 0; i < len(shape); i++ {
			if p <= int64(shape[i].utilization) {
				if i == 0 {
					return shape[0].score
				}
				return shape[i-1].score + (shape[i].score-shape[i-1].score)*(p-shape[i-1].utilization)/(shape[i].utilization-shape[i-1].utilization)
			}
		}
		return shape[len(shape)-1].score
	}
}
