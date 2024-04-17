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
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v13 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	katalyst_base "github.com/kubewharf/katalyst-core/cmd/base"
	"github.com/kubewharf/katalyst-core/pkg/client/control"
	"github.com/kubewharf/katalyst-core/pkg/config/controller"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/common"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/predictor"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/prediction/provider/prom"
)

func TestInitPredictor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		conf      *controller.OvercommitConfig
		expectErr bool
	}{
		{
			name: "nsigma",
			conf: &controller.OvercommitConfig{
				Prediction: controller.PredictionConfig{
					Predictor: "NSigma",
				},
			},
			expectErr: false,
		},
		{
			name:      "skip",
			conf:      &controller.OvercommitConfig{},
			expectErr: false,
		},
		{
			name: "unknow",
			conf: &controller.OvercommitConfig{
				Prediction: controller.PredictionConfig{
					Predictor: "test",
				},
			},
			expectErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Prediction{}
			p.conf = tc.conf

			err := p.initPredictor()
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestReconcileWorkloads(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		workload          interface{}
		rpr               *v1alpha1.Portrait
		conf              *controller.OvercommitConfig
		timeSeries        *common.TimeSeries
		predictTimeSeries *common.TimeSeries
		expectRprLen      int
	}{
		{
			name: "patch resource portrait result",
			workload: &v13.Deployment{
				TypeMeta: v12.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: v12.ObjectMeta{
					Name:      "testDeployment",
					Namespace: "default",
				},
			},
			rpr: &v1alpha1.Portrait{
				TypeMeta: v12.TypeMeta{
					Kind:       "ResourcePortrait",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: v12.ObjectMeta{
					Namespace: "overcommit",
					Name:      resourcePortraitResultName("testDeployment", "Deployment", "default"),
				},
			},
			conf: &controller.OvercommitConfig{
				Prediction: controller.PredictionConfig{
					MinTimeSeriesDuration:     time.Minute,
					ResourcePortraitNamespace: "overcommit",
					NSigmaPredictorConfig: controller.NSigmaPredictorConfig{
						Factor:  3,
						Buckets: 24,
					},
				},
			},
			timeSeries:        generateRandTimeSeries(),
			predictTimeSeries: generatePredictTimeSeries(),
			expectRprLen:      24,
		},
		{
			name: "create resource portrait result",
			workload: &v13.Deployment{
				TypeMeta: v12.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: v12.ObjectMeta{
					Name:      "testDeployment",
					Namespace: "default",
				},
			},
			rpr: nil,
			conf: &controller.OvercommitConfig{
				Prediction: controller.PredictionConfig{
					MinTimeSeriesDuration:     time.Minute,
					ResourcePortraitNamespace: "overcommit",
					NSigmaPredictorConfig: controller.NSigmaPredictorConfig{
						Factor:  3,
						Buckets: 24,
					},
				},
			},
			timeSeries:        generateRandTimeSeries(),
			predictTimeSeries: generatePredictTimeSeries(),
			expectRprLen:      24,
		},
		{
			name: "validate fail",
			workload: &v13.Deployment{
				TypeMeta: v12.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: v12.ObjectMeta{
					Name:      "testDeployment",
					Namespace: "default",
				},
			},
			rpr: &v1alpha1.Portrait{
				TypeMeta: v12.TypeMeta{
					Kind:       "ResourcePortrait",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: v12.ObjectMeta{
					Namespace: "overcommit",
					Name:      resourcePortraitResultName("testDeployment", "Deployment", "default"),
				},
				TimeSeries: map[string][]v1alpha1.Item{
					"cpu_utilization_usage_seconds_max": {},
				},
			},
			conf: &controller.OvercommitConfig{
				Prediction: controller.PredictionConfig{
					MinTimeSeriesDuration:     48 * time.Hour,
					ResourcePortraitNamespace: "overcommit",
					NSigmaPredictorConfig: controller.NSigmaPredictorConfig{
						Factor:  3,
						Buckets: 24,
					},
				},
			},
			timeSeries:        generateRandTimeSeries(),
			predictTimeSeries: generatePredictTimeSeries(),
			expectRprLen:      0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workload := tc.workload.(*v13.Deployment)
			controlCtx, err := katalyst_base.GenerateFakeGenericContext([]runtime.Object{},
				[]runtime.Object{}, []runtime.Object{workload})
			assert.NoError(t, err)

			controller, err := newTestController(context.TODO(), controlCtx, tc.conf)
			assert.NoError(t, err)

			if tc.rpr != nil {
				_, err = controller.rpUpdater.CreateRpr(context.TODO(), tc.conf.Prediction.ResourcePortraitNamespace, tc.rpr)
				assert.NoError(t, err)
			}

			controlCtx.StartInformer(context.TODO())
			go controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Informer().Run(context.TODO().Done())
			synced := cache.WaitForCacheSync(context.TODO().Done(), controller.syncedFunc...)
			assert.True(t, synced)

			controller.provider = prom.NewFakeProvider([]*common.TimeSeries{tc.timeSeries})
			controller.predictor = predictor.NewFakePredictor(tc.predictTimeSeries)

			controller.reconcileWorkloads()

			result, err := controlCtx.Client.InternalClient.ResourceportraitV1alpha1().
				Portraits(tc.conf.Prediction.ResourcePortraitNamespace).Get(context.TODO(), resourcePortraitResultName(workload.Name, workload.Kind, "default"), v12.GetOptions{})
			assert.NoError(t, err)

			res := result.TimeSeries["cpu_utilization_usage_seconds_max"]
			assert.Equal(t, tc.expectRprLen, len(res))
		})
	}
}

func TestDeleteResourcePortraitResult(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		workload *v13.Deployment
		rpr      *v1alpha1.Portrait
		conf     *controller.OvercommitConfig
	}{
		{
			name: "test1",
			workload: &v13.Deployment{
				TypeMeta: v12.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: v12.ObjectMeta{
					Name:      "testDeployment",
					Namespace: "default",
				},
			},
			rpr: &v1alpha1.Portrait{
				TypeMeta: v12.TypeMeta{
					Kind:       "ResourcePortrait",
					APIVersion: "v1alpha1",
				},
				ObjectMeta: v12.ObjectMeta{
					Namespace: "overcommit",
					Name:      resourcePortraitResultName("testDeployment", "Deployment", "default"),
				},
				TimeSeries: map[string][]v1alpha1.Item{
					"cpu_utilization_usage_seconds_max": {},
				},
			},
			conf: &controller.OvercommitConfig{
				Prediction: controller.PredictionConfig{
					ResourcePortraitNamespace: "overcommit",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workload := tc.workload
			controlCtx, err := katalyst_base.GenerateFakeGenericContext([]runtime.Object{},
				[]runtime.Object{tc.rpr}, []runtime.Object{workload})
			assert.NoError(t, err)

			controller, err := newTestController(context.TODO(), controlCtx, tc.conf)
			assert.NoError(t, err)
			controller.provider = prom.NewFakeProvider(nil)
			controller.predictor = predictor.NewFakePredictor(nil)

			controller.addDeleteHandler(controlCtx.DynamicResourcesManager.GetDynamicInformers())

			controlCtx.StartInformer(context.TODO())

			synced := cache.WaitForCacheSync(context.TODO().Done(), controller.syncedFunc...)
			assert.True(t, synced)

			rpr, err := controlCtx.Client.InternalClient.ResourceportraitV1alpha1().Portraits("overcommit").Get(context.TODO(), resourcePortraitResultName(workload.Name, workload.Kind, "default"), v12.GetOptions{})
			assert.NoError(t, err)
			t.Log(rpr)

			err = controlCtx.Client.DynamicClient.Resource(deploymentGVR).Namespace(workload.Namespace).Delete(context.TODO(), workload.Name, v12.DeleteOptions{})
			assert.NoError(t, err)

			_, err = controlCtx.Client.DynamicClient.Resource(deploymentGVR).Namespace(workload.Namespace).Get(context.TODO(), workload.Name, v12.GetOptions{})
			assert.Error(t, err)

			time.Sleep(time.Second)
			_, err = controlCtx.Client.InternalClient.ResourceportraitV1alpha1().Portraits("overcommit").Get(context.TODO(), resourcePortraitResultName(workload.Name, workload.Kind, "default"), v12.GetOptions{})
			assert.True(t, errors.IsNotFound(err))
		})
	}
}

func newTestController(
	ctx context.Context,
	controlCtx *katalyst_base.GenericContext,
	overcommitConf *controller.OvercommitConfig) (*Prediction, error) {

	predictionController := &Prediction{
		ctx:            ctx,
		metricsEmitter: controlCtx.EmitterPool.GetDefaultMetricsEmitter(),
		conf:           overcommitConf,
		workloadLister: map[schema.GroupVersionResource]cache.GenericLister{},
	}

	workloadInformers := controlCtx.DynamicResourcesManager.GetDynamicInformers()
	for _, wf := range workloadInformers {
		predictionController.workloadLister[wf.GVR] = wf.Informer.Lister()
		predictionController.syncedFunc = append(predictionController.syncedFunc, wf.Informer.Informer().HasSynced)
	}

	predictionController.resourcePortraitLister = controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Lister()
	predictionController.syncedFunc = append(predictionController.syncedFunc, controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Informer().HasSynced)

	predictionController.nodeLister = controlCtx.KubeInformerFactory.Core().V1().Nodes().Lister()
	predictionController.syncedFunc = append(predictionController.syncedFunc, controlCtx.KubeInformerFactory.Core().V1().Nodes().Informer().HasSynced)
	predictionController.podIndexer = controlCtx.KubeInformerFactory.Core().V1().Pods().Informer().GetIndexer()
	predictionController.podIndexer.AddIndexers(cache.Indexers{
		nodePodIndex: nodePodIndexFunc,
	})
	predictionController.nodeUpdater = control.NewRealNodeUpdater(controlCtx.Client.KubeClient)
	predictionController.rpUpdater = control.NewRealRprUpdater(controlCtx.Client.InternalClient)

	return predictionController, nil
}

func generateRandTimeSeries() *common.TimeSeries {
	rand.Seed(time.Now().UnixNano())
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	nextDay := day.Add(24 * time.Hour)

	res := common.EmptyTimeSeries()
	for day.Before(nextDay) {
		res.Samples = append(res.Samples, common.Sample{
			Value:     rand.Float64() * 4,
			Timestamp: day.Unix(),
		})

		day = day.Add(1 * time.Minute)
	}

	return res
}

func generatePredictTimeSeries() *common.TimeSeries {
	now := time.Now()
	startTime := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)

	res := common.EmptyTimeSeries()
	for i := 0; i < 24; i++ {
		res.Samples = append(res.Samples, common.Sample{
			Value:     rand.Float64() * 4,
			Timestamp: startTime.Unix(),
		})

		startTime = startTime.Add(1 * time.Hour)
	}

	return res
}
