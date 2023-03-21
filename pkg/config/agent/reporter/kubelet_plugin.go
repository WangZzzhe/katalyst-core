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

package reporter

import (
	"github.com/kubewharf/katalyst-core/pkg/config/dynamic"
)

type KubeletPluginConfiguration struct {
	PodResourcesServerEndpoints []string
}

func NewKubeletPluginConfiguration() *KubeletPluginConfiguration {
	return &KubeletPluginConfiguration{}
}

func (c *KubeletPluginConfiguration) ApplyConfiguration(_ *dynamic.DynamicConfigCRD) {}