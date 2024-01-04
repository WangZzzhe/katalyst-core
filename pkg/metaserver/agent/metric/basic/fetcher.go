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

package basic

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"

	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric"
	"github.com/kubewharf/katalyst-core/pkg/util/machine"
	utilmetric "github.com/kubewharf/katalyst-core/pkg/util/metric"
)

type Fetcher struct {
	sync.RWMutex
	registeredMetric   []func(store *utilmetric.MetricStore)
	registeredNotifier map[metric.MetricsScope]map[string]metric.NotifiedData

	MetricStore *utilmetric.MetricStore
}

func NewFetcher() *Fetcher {
	return &Fetcher{
		MetricStore: utilmetric.NewMetricStore(),

		registeredMetric: make([]func(store *utilmetric.MetricStore), 0),
		registeredNotifier: map[metric.MetricsScope]map[string]metric.NotifiedData{
			metric.MetricsScopeNode:      make(map[string]metric.NotifiedData),
			metric.MetricsScopeNuma:      make(map[string]metric.NotifiedData),
			metric.MetricsScopeCPU:       make(map[string]metric.NotifiedData),
			metric.MetricsScopeDevice:    make(map[string]metric.NotifiedData),
			metric.MetricsScopeContainer: make(map[string]metric.NotifiedData),
		},
	}
}

func (f *Fetcher) RegisterNotifier(
	scope metric.MetricsScope,
	req metric.NotifiedRequest,
	response chan metric.NotifiedResponse,
) string {
	if _, ok := f.registeredNotifier[scope]; !ok {
		return ""
	}

	f.Lock()
	defer f.Unlock()

	randBytes := make([]byte, 30)
	rand.Read(randBytes)
	key := string(randBytes)

	f.registeredNotifier[scope][key] = metric.NotifiedData{
		Scope:    scope,
		Req:      req,
		Response: response,
	}
	return key
}

func (f *Fetcher) DeRegisterNotifier(scope metric.MetricsScope, key string) {
	f.Lock()
	defer f.Unlock()

	delete(f.registeredNotifier[scope], key)
}

func (f *Fetcher) RegisterExternalMetric(externalFunc func(store *utilmetric.MetricStore)) {
	f.Lock()
	defer f.Unlock()
	f.registeredMetric = append(f.registeredMetric, externalFunc)
}

func (f *Fetcher) UpdateRegisteredMetric() {
	f.RLock()
	defer f.RUnlock()

	for _, registeredFunc := range f.registeredMetric {
		registeredFunc(f.MetricStore)
	}
}

func (f *Fetcher) GetNodeMetric(metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetNodeMetric(metricName)
}

func (f *Fetcher) GetNumaMetric(numaID int, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetNumaMetric(numaID, metricName)
}

func (f *Fetcher) GetDeviceMetric(deviceName string, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetDeviceMetric(deviceName, metricName)
}

func (f *Fetcher) GetCPUMetric(coreID int, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetCPUMetric(coreID, metricName)
}

func (f *Fetcher) GetContainerMetric(podUID, containerName, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetContainerMetric(podUID, containerName, metricName)
}

func (f *Fetcher) GetContainerNumaMetric(podUID, containerName, numaNode, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetContainerNumaMetric(podUID, containerName, numaNode, metricName)
}

func (f *Fetcher) GetCgroupMetric(cgroupPath, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetCgroupMetric(cgroupPath, metricName)
}

func (f *Fetcher) GetCgroupNumaMetric(cgroupPath, numaNode, metricName string) (utilmetric.MetricData, error) {
	return f.MetricStore.GetCgroupNumaMetric(cgroupPath, numaNode, metricName)
}

func (f *Fetcher) AggregatePodNumaMetric(podList []*v1.Pod, numaNode, metricName string,
	agg utilmetric.Aggregator, filter utilmetric.ContainerMetricFilter) utilmetric.MetricData {
	return f.MetricStore.AggregatePodNumaMetric(podList, numaNode, metricName, agg, filter)
}

func (f *Fetcher) AggregatePodMetric(podList []*v1.Pod, metricName string,
	agg utilmetric.Aggregator, filter utilmetric.ContainerMetricFilter) utilmetric.MetricData {
	return f.MetricStore.AggregatePodMetric(podList, metricName, agg, filter)
}

func (f *Fetcher) AggregateCoreMetric(cpuset machine.CPUSet, metricName string, agg utilmetric.Aggregator) utilmetric.MetricData {
	return f.MetricStore.AggregateCoreMetric(cpuset, metricName, agg)
}

func (f *Fetcher) NotifySystem() {
	now := time.Now()
	f.RLock()
	defer f.RUnlock()

	for _, reg := range f.registeredNotifier[metric.MetricsScopeNode] {
		v, err := f.MetricStore.GetNodeMetric(reg.Req.MetricName)
		if err != nil {
			continue
		} else if v.Time == nil {
			v.Time = &now
		}
		reg.Response <- metric.NotifiedResponse{
			Req:        reg.Req,
			MetricData: v,
		}
	}

	for _, reg := range f.registeredNotifier[metric.MetricsScopeDevice] {
		v, err := f.MetricStore.GetDeviceMetric(reg.Req.DeviceID, reg.Req.MetricName)
		if err != nil {
			continue
		} else if v.Time == nil {
			v.Time = &now
		}
		reg.Response <- metric.NotifiedResponse{
			Req:        reg.Req,
			MetricData: v,
		}
	}

	for _, reg := range f.registeredNotifier[metric.MetricsScopeNuma] {
		v, err := f.MetricStore.GetNumaMetric(reg.Req.NumaID, reg.Req.MetricName)
		if err != nil {
			continue
		} else if v.Time == nil {
			v.Time = &now
		}
		reg.Response <- metric.NotifiedResponse{
			Req:        reg.Req,
			MetricData: v,
		}
	}

	for _, reg := range f.registeredNotifier[metric.MetricsScopeCPU] {
		v, err := f.MetricStore.GetCPUMetric(reg.Req.CoreID, reg.Req.MetricName)
		if err != nil {
			continue
		} else if v.Time == nil {
			v.Time = &now
		}
		reg.Response <- metric.NotifiedResponse{
			Req:        reg.Req,
			MetricData: v,
		}
	}
}

func (f *Fetcher) NotifyPods() {
	now := time.Now()
	f.RLock()
	defer f.RUnlock()

	for _, reg := range f.registeredNotifier[metric.MetricsScopeContainer] {
		v, err := f.MetricStore.GetContainerMetric(reg.Req.PodUID, reg.Req.ContainerName, reg.Req.MetricName)
		if err != nil {
			continue
		} else if v.Time == nil {
			v.Time = &now
		}
		reg.Response <- metric.NotifiedResponse{
			Req:        reg.Req,
			MetricData: v,
		}

		if reg.Req.NumaID == 0 {
			continue
		}

		v, err = f.MetricStore.GetContainerNumaMetric(reg.Req.PodUID, reg.Req.ContainerName, fmt.Sprintf("%v", reg.Req.NumaID), reg.Req.MetricName)
		if err != nil {
			continue
		} else if v.Time == nil {
			v.Time = &now
		}
		reg.Response <- metric.NotifiedResponse{
			Req:        reg.Req,
			MetricData: v,
		}
	}
}
