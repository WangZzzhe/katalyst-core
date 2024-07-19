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

package consts

// HaloCustomConfigAnnotationKeyConfigHash defines const variables for kcc annotations about config hash.
const (
	HaloCustomConfigAnnotationKeyConfigHash = "kcc.halo.io/config.hash"
)

// HaloCustomConfigFinalizerKCC defines const variables for kcc finalizer
const (
	HaloCustomConfigFinalizerKCC = "kcc.halo.io/kcc-controller"
)

// const variables for kcc target finalizer
const (
	HaloCustomConfigTargetFinalizerKCC  = "kcct.halo.io/kcc-controller"
	HaloCustomConfigTargetFinalizerCNC  = "kcct.halo.io/cnc-controller"
	HaloCustomConfigTargetFinalizerKCCT = "kcct.halo.io/kcct-controller"
)

// generic spec fields for configuration CRD (referred by KCC)
const (
	KCCTargetConfFieldNameLabelSelector        = "nodeLabelSelector"
	KCCTargetConfFieldNamePriority             = "priority"
	KCCTargetConfFieldEphemeralSelector        = "ephemeralSelector"
	KCCTargetConfFieldNameRevisionHistoryLimit = "revisionHistoryLimit"
	KCCTargetConfFieldNameConfig               = "config"

	KCCTargetConfFieldNameNodeNames    = "nodeNames"
	KCCTargetConfFieldNameLastDuration = "lastDuration"
)

// generic status fields for configuration CRD (referred by KCC)
const (
	KCCTargetConfFieldNameCollisionCount     = "collisionCount"
	KCCTargetConfFieldNameObservedGeneration = "observedGeneration"
)
