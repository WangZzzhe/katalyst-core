package loadaware

import (
	"context"
	"github.com/kubewharf/katalyst-api/pkg/apis/scheduling/config"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cache2 "k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	"github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	katalyst_base "github.com/kubewharf/katalyst-core/cmd/base"
	"github.com/kubewharf/katalyst-core/pkg/config/generic"
	"github.com/kubewharf/katalyst-core/pkg/scheduler/util"
)

func TestFitByPortrait(t *testing.T) {
	t.Parallel()

	util.SetQoSConfig(generic.NewQoSConfiguration())

	for _, tc := range []struct {
		name      string
		pod       *v1.Pod
		node      *v1.Node
		pods      []*v1.Pod
		portraits []*v1alpha1.Portrait
		expectRes *framework.Status
	}{
		{
			name: "",
			pod: &v1.Pod{
				ObjectMeta: v12.ObjectMeta{
					Name:      "pod1",
					UID:       "pod1UID",
					Namespace: "testNs",
					OwnerReferences: []v12.OwnerReference{
						{
							Kind: "Deployment",
							Name: "deployment1",
						},
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "testContainer",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("16Gi"),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("16Gi"),
								},
							},
						},
					},
				},
			},
			node: &v1.Node{
				ObjectMeta: v12.ObjectMeta{
					Name: "node1",
				},
				Spec: v1.NodeSpec{},
				Status: v1.NodeStatus{
					Capacity: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU:    resource.MustParse("32"),
						v1.ResourceMemory: resource.MustParse("64Gi"),
					},
					Allocatable: map[v1.ResourceName]resource.Quantity{
						v1.ResourceCPU:    resource.MustParse("32"),
						v1.ResourceMemory: resource.MustParse("64Gi"),
					},
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      "pod2",
						UID:       "pod2UID",
						Namespace: "testNs",
						OwnerReferences: []v12.OwnerReference{
							{
								Kind: "Deployment",
								Name: "deployment2",
							},
						},
					},
				},
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      "pod3",
						UID:       "pod3UID",
						Namespace: "testNs",
						OwnerReferences: []v12.OwnerReference{
							{
								Kind: "Deployment",
								Name: "deployment3",
							},
						},
					},
				},
			},
			portraits: []*v1alpha1.Portrait{
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      resourcePortraitResultName("deployment1", "Deployment", portraitNameFmt, portraitAppName),
						Namespace: "testNs",
					},
					TimeSeries: map[string][]v1alpha1.Item{
						cpuUsageMetric:    rangeItems(4),
						memoryUsageMetric: rangeItems(8 * 1024 * 1024 * 1024),
					},
				},
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      resourcePortraitResultName("deployment2", "Deployment", portraitNameFmt, portraitAppName),
						Namespace: "testNs",
					},
					TimeSeries: map[string][]v1alpha1.Item{
						cpuUsageMetric:    fixedItems(4),
						memoryUsageMetric: fixedItems(8 * 1024 * 1024 * 1024),
					},
				},
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      resourcePortraitResultName("deployment3", "Deployment", portraitNameFmt, portraitAppName),
						Namespace: "testNs",
					},
					TimeSeries: map[string][]v1alpha1.Item{
						cpuUsageMetric:    fixedItems(8),
						memoryUsageMetric: fixedItems(16 * 1024 * 1024 * 1024),
					},
				},
			},
			expectRes: nil,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nodeInfo := framework.NewNodeInfo()
			nodeInfo.SetNode(tc.node)
			for _, pod := range tc.pods {
				nodeInfo.AddPod(pod)
			}
			fw, err := runtime.NewFramework(nil, nil,
				runtime.WithSnapshotSharedLister(newTestSharedLister(tc.pods, []*v1.Node{tc.node})))
			assert.NoError(t, err)

			controlCtx, err := katalyst_base.GenerateFakeGenericContext()
			assert.NoError(t, err)

			p := &Plugin{
				handle:            fw,
				args:              makeTestArgs(),
				portraitLister:    controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Lister(),
				portraitHasSynced: controlCtx.InternalInformerFactory.Resourceportrait().V1alpha1().Portraits().Informer().HasSynced,
			}
			cache.SetPortraitLister(p)

			for _, pr := range tc.portraits {
				_, err = controlCtx.Client.InternalClient.ResourceportraitV1alpha1().Portraits(pr.Namespace).
					Create(context.TODO(), pr, v12.CreateOptions{})
				assert.NoError(t, err)
			}
			controlCtx.StartInformer(context.TODO())

			// wait for portrait synced
			if !cache2.WaitForCacheSync(context.TODO().Done(), p.portraitHasSynced) {
				t.Error("wait for portrait informer synced fail")
				t.FailNow()
			}

			// add pod to cache
			for _, pod := range tc.pods {
				cache.addPod(tc.node.Name, pod, time.Now())
			}

			status := p.fitByPortrait(tc.pod, nodeInfo)

			if tc.expectRes == nil {
				assert.Nil(t, status)
			} else {
				assert.Equal(t, tc.expectRes.Code(), status.Code())
			}
		})
	}
}

func fixedItems(value float64) []v1alpha1.Item {
	res := make([]v1alpha1.Item, portraitItemsLength, portraitItemsLength)

	t := time.Now()
	for i := 0; i < portraitItemsLength; i++ {
		res[i].Timestamp = t.Add(time.Duration(i) * time.Hour).Unix()
		res[i].Value = value
	}
	return res
}

func rangeItems(maxValue float64) []v1alpha1.Item {
	res := make([]v1alpha1.Item, portraitItemsLength, portraitItemsLength)

	t := time.Now()
	rand.Seed(t.UnixNano())
	for i := 0; i < portraitItemsLength; i++ {
		res[i].Timestamp = t.Add(time.Duration(i) * time.Hour).Unix()
		res[i].Value = rand.Float64() * maxValue
	}

	return res
}

func makeTestArgs() *config.LoadAwareArgs {
	args := &config.LoadAwareArgs{
		EnablePortrait: true,
		ResourceToTargetMap: map[v1.ResourceName]int64{
			v1.ResourceCPU:    40,
			v1.ResourceMemory: 50,
		},
		ResourceToThresholdMap: map[v1.ResourceName]int64{
			v1.ResourceCPU:    60,
			v1.ResourceMemory: 80,
		},
		ResourceToScalingFactorMap: map[v1.ResourceName]int64{
			v1.ResourceCPU:    100,
			v1.ResourceMemory: 100,
		},
		ResourceToWeightMap: map[v1.ResourceName]int64{
			v1.ResourceCPU:    1,
			v1.ResourceMemory: 1,
		},
	}
	args.PodAnnotationLoadAwareEnable = new(string)
	*args.PodAnnotationLoadAwareEnable = ""

	return args
}