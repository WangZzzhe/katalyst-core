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

package prediction

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/common"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/predictor/nsigma"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/provider/prom"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

func (p *Prediction) initPredictor() error {
	switch p.conf.Prediction.Predictor {
	case nsigma.NSigmaPredictor:
		p.predictor = nsigma.NewPredictor(p.conf.Prediction.Factor, p.conf.Prediction.Buckets)
	case "":
		klog.Infof("predictor not set, skip")
	default:
		return fmt.Errorf("predictor %v not support yet", p.conf.Prediction.Predictor)
	}

	return nil
}

func (p *Prediction) initProvider() error {
	if p.conf.Prediction.Address == "" {
		klog.Warning("provider address not set")
		return nil
	}
	// init provider
	promProvider, err := prom.NewProvider(
		p.conf.Prediction.Address,
		p.conf.Prediction.ClientAuth,
		p.conf.Prediction.MaxPointsLimit)
	if err != nil {
		err = fmt.Errorf("new prom provider fail: %v", err)
		return err
	}
	p.provider = promProvider
	return nil
}

// predict workloads cpu and memory usage by history time series
func (p *Prediction) reconcileWorkloads() {
	for gvr, lister := range p.workloadLister {
		objs, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("workload %v list fail: %v", gvr.String(), err)
			continue
		}
		klog.V(6).Infof("reconcile %v workload, length: %v", gvr.String(), len(objs))

		for _, obj := range objs {
			err := p.predictWorkload(obj)
			if err != nil {
				klog.Error(err)
				continue
			}
		}
	}
	p.firstReconcileWorkload = true
}

func (p *Prediction) generateQuery(resourceName string, workload *unstructured.Unstructured) (string, error) {
	matchLabels := []common.Metadata{
		{Key: namespaceMatchKey, Value: workload.GetNamespace()},
		{Key: podMatchKey, Value: podNameByWorkload(workload.GetName(), workload.GetKind())},
		{Key: containerMatchKey, Value: ""},
	}

	return p.provider.BuildQuery(resourceName, matchLabels)
}

func (p *Prediction) requestHistoryTimeSeries(query string) ([]*common.TimeSeries, error) {
	now := time.Now()
	endTime := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.Local)
	startTime := endTime.Add(-p.conf.Prediction.MaxTimeSeriesDuration)

	return p.provider.QueryTimeSeries(p.ctx, query, startTime, endTime, defaultStep)
}

func (p *Prediction) validateTimeSeries(timeSeries []*common.TimeSeries) (bool, error) {
	if len(timeSeries) <= 0 {
		return false, fmt.Errorf("timeSeries without data")
	}

	if len(timeSeries) > 1 {
		return false, fmt.Errorf("more than 1 timeSeries")
	}

	startTime := timeSeries[0].Samples[0].Timestamp
	endTime := timeSeries[0].Samples[len(timeSeries[0].Samples)-1].Timestamp

	if p.conf.Prediction.MinTimeSeriesDuration > time.Duration(endTime-startTime)*time.Second {
		return false, fmt.Errorf("not enough data, startTime: %v, endTime: %v, minDuration: %v", startTime, endTime, p.conf.Prediction.MinTimeSeriesDuration)
	}

	return true, nil
}

func (p *Prediction) predictWorkload(obj runtime.Object) error {
	workload := obj.(*unstructured.Unstructured)

	predictTimeSeriesRes := make(map[string]*common.TimeSeries, 0)
	for resource, metricName := range resourceToPortraitMetrics {
		// generate query
		query, err := p.generateQuery(resource, workload)
		if err != nil {
			klog.Errorf("%v %v generateQuery fail: %v", workload.GetKind(), workload.GetName(), err)
			return err
		}

		// request metrics
		timeSeries, err := p.requestHistoryTimeSeries(query)
		if err != nil {
			klog.Errorf("%v %v requestHistoryTimeSeries fail: %v", workload.GetKind(), workload.GetName(), err)
			return err
		}

		// validate timeSeries
		ok, err := p.validateTimeSeries(timeSeries)
		if !ok {
			klog.V(6).Infof("workload %v validateTimeSeries: %v", workload.GetName(), err)
			return err
		}

		// predict
		predictTimeSeries, err := p.predictor.PredictTimeSeries(p.ctx, &common.PredictArgs{WorkloadName: workload.GetName()}, timeSeries[0])
		if err != nil {
			klog.Errorf("%v %v PredictTimeSeries fail: %v", workload.GetKind(), workload.GetName(), err)
			return err
		}

		klog.V(6).Infof("%v %v predict timeSeries: %v", workload.GetKind(), workload.GetName(), predictTimeSeries.Samples)
		predictTimeSeriesRes[metricName] = predictTimeSeries
	}

	err := p.updateResourcePortraitResult(workload.GetNamespace(), workload.GetName(), workload.GetKind(), predictTimeSeriesRes)
	if err != nil {
		klog.Errorf("%v %v update resource portrait result fail: %v", workload.GetKind(), workload.GetName(), err)
	}

	return nil
}

func (p *Prediction) updateResourcePortraitResult(namespace string, workloadName string, workloadType string, metricTimeSeries map[string]*common.TimeSeries) error {
	if len(metricTimeSeries) <= 0 {
		return fmt.Errorf("update workload %v portrait result, get null timeSeries", workloadName)
	}

	resultName := resourcePortraitResultName(workloadName, workloadType, namespace)

	result, err := p.resourcePortraitLister.Portraits(p.conf.Prediction.ResourcePortraitNamespace).Get(resultName)
	if err != nil {
		if errors.IsNotFound(err) {
			// create result
			result = &v1alpha1.Portrait{
				ObjectMeta: v1.ObjectMeta{
					Namespace: p.conf.Prediction.ResourcePortraitNamespace,
					Name:      resultName,
					Labels: map[string]string{
						portraitManagedLabelKey: portraitManagedLabelValFalse,
					},
				},
				TimeSeries: map[string][]v1alpha1.Item{},
			}
			for metric, timeSeries := range metricTimeSeries {
				result.TimeSeries[metric] = timeSeriesToItems(timeSeries)
			}

			_, err = p.rpUpdater.CreateRpr(p.ctx, p.conf.Prediction.ResourcePortraitNamespace, result)
			if err != nil {
				klog.Errorf("create rpr %v fail: %v", result.Name, err)
				return err
			}
			return nil
		} else {
			return err
		}
	}

	// patch result
	resultCopy := result.DeepCopy()
	if resultCopy.TimeSeries == nil {
		resultCopy.TimeSeries = map[string][]v1alpha1.Item{}
	}
	for metric, timeSeries := range metricTimeSeries {
		resultCopy.TimeSeries[metric] = timeSeriesToItems(timeSeries)
	}
	_, err = p.rpUpdater.PatchRpr(p.ctx, p.conf.Prediction.ResourcePortraitNamespace, result, resultCopy)
	if err != nil {
		klog.Errorf("patch rpr %v fali: %v", result.Name, err)
		return err
	}

	return nil
}

func timeSeriesToItems(timeSeries *common.TimeSeries) []v1alpha1.Item {
	res := make([]v1alpha1.Item, 0)
	for i := range timeSeries.Samples {
		res = append(res, v1alpha1.Item{
			Value:     timeSeries.Samples[i].Value,
			Timestamp: timeSeries.Samples[i].Timestamp,
		})
	}

	return res
}

func (p *Prediction) addDeleteHandler(informers map[string]native.DynamicInformer) {
	if p.predictor != nil && p.provider != nil {
		for _, informer := range informers {
			informer.Informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
				DeleteFunc: p.deleteResourcePortraitResult,
			})
		}
	}
}

func (p *Prediction) deleteResourcePortraitResult(obj interface{}) {
	object, ok := obj.(*unstructured.Unstructured)
	if !ok {
		klog.Errorf("cannot convert obj to Unstructured: %v", obj)
		return
	}

	timeout, cancel := context.WithTimeout(p.ctx, time.Second)
	defer cancel()

	name := resourcePortraitResultName(object.GetName(), object.GetKind(), object.GetNamespace())
	err := p.rpUpdater.DeleteRpr(timeout, p.conf.Prediction.ResourcePortraitNamespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return
		}
		klog.Errorf("delete resource portrait result %v fail: %v", name, err)
	}
}
