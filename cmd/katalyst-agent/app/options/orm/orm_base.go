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

package orm

import (
	"time"

	cliflag "k8s.io/component-base/cli/flag"

	ormconfig "github.com/kubewharf/katalyst-core/pkg/config/agent/orm"
)

type GenericORMPluginOptions struct {
	ORMRconcilePeriod   time.Duration
	ORMResourceNamesMap map[string]string
	ORMPodNotifyChanLen int
}

func NewGenericORMPluginOptions() *GenericORMPluginOptions {
	return &GenericORMPluginOptions{
		ORMRconcilePeriod:   time.Second * 5,
		ORMResourceNamesMap: map[string]string{},
		ORMPodNotifyChanLen: 10,
	}
}

func (o *GenericORMPluginOptions) AddFlags(fss *cliflag.NamedFlagSets) {
	fs := fss.FlagSet("orm")

	fs.DurationVar(&o.ORMRconcilePeriod, "orm-reconcile-period",
		o.ORMRconcilePeriod, "orm resource reconcile period")
	fs.Var(cliflag.NewMapStringString(&o.ORMResourceNamesMap), "orm-resource-names-map",
		"A set of ResourceName=ResourceQuantity pairs that map resource name during QoS Resource Manager allocation period. "+
			"e.g. 'resource.katalyst.kubewharf.io/reclaimed_millicpu=cpu,resource.katalyst.kubewharf.io/reclaimed_memory=memory' "+
			"should be set for that reclaimed_cores pods with resources [resource.katalyst.kubewharf.io/reclaimed_millicpu] and [resource.katalyst.kubewharf.io/reclaimed_memory]"+
			"will also be allocated by [cpu] and [memory] QRM plugins")
	fs.IntVar(&o.ORMPodNotifyChanLen, "orm-pod-notify-chan-len",
		o.ORMPodNotifyChanLen, "length of pod addition and movement notifying channel")
}

func (o *GenericORMPluginOptions) ApplyTo(conf *ormconfig.GenericORMConfiguration) error {
	conf.ORMRconcilePeriod = o.ORMRconcilePeriod
	conf.ORMResourceNamesMap = o.ORMResourceNamesMap
	conf.ORMPodNotifyChanLen = o.ORMPodNotifyChanLen

	return nil
}
