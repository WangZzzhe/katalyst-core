package loadaware

import (
	"context"
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

func TestTargetLoadPacking(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		targetRatio float64
		usageRatio  float64
		expectErr   bool
		expectRes   int64
	}{
		{
			name:        "less than target",
			targetRatio: 50,
			usageRatio:  10,
			expectErr:   false,
			expectRes:   60,
		},
		{
			name:        "greater than target",
			targetRatio: 50,
			usageRatio:  60,
			expectErr:   false,
			expectRes:   40,
		},
		{
			name:        "zero target",
			targetRatio: 0,
			usageRatio:  10,
			expectErr:   true,
			expectRes:   0,
		},
		{
			name:        "target greater than 100",
			targetRatio: 200,
			usageRatio:  10,
			expectErr:   true,
			expectRes:   0,
		},
		{
			name:        "usage less than 0",
			targetRatio: 50,
			usageRatio:  -1,
			expectErr:   false,
			expectRes:   50,
		},
		{
			name:        "usage greater than 100",
			targetRatio: 50,
			usageRatio:  101,
			expectErr:   false,
			expectRes:   0,
		},
		{
			name:        "low usage",
			targetRatio: 30,
			usageRatio:  0.1,
			expectErr:   false,
			expectRes:   30,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := targetLoadPacking(tc.targetRatio, tc.usageRatio)
			if !tc.expectErr {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.expectRes, res)
		})
	}
}

func TestScoreByPortrait(t *testing.T) {
	t.Parallel()

	util.SetQoSConfig(generic.NewQoSConfiguration())

	for _, tc := range []struct {
		name         string
		pod          *v1.Pod
		lowNode      *v1.Node
		highNode     *v1.Node
		lowNodePods  []*v1.Pod
		highNodePods []*v1.Pod
		portraits    []*v1alpha1.Portrait
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
			lowNode: &v1.Node{
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
			highNode: &v1.Node{
				ObjectMeta: v12.ObjectMeta{
					Name: "node2",
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
			lowNodePods: []*v1.Pod{
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      "pod2",
						UID:       "pod2UID",
						Namespace: "testNs",
						OwnerReferences: []v12.OwnerReference{
							{
								Kind: "Deployment",
								Name: "deployment3",
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
			highNodePods: []*v1.Pod{
				{
					ObjectMeta: v12.ObjectMeta{
						Name:      "pod5",
						UID:       "pod5UID",
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
						Name:      "pod4",
						UID:       "pod4UID",
						Namespace: "testNs",
						OwnerReferences: []v12.OwnerReference{
							{
								Kind: "Deployment",
								Name: "deployment2",
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
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lowNodeInfo := framework.NewNodeInfo()
			lowNodeInfo.SetNode(tc.lowNode)
			for _, pod := range tc.lowNodePods {
				lowNodeInfo.AddPod(pod)
			}

			highNodeInfo := framework.NewNodeInfo()
			highNodeInfo.SetNode(tc.highNode)
			for _, pod := range tc.highNodePods {
				highNodeInfo.AddPod(pod)
			}

			fw, err := runtime.NewFramework(nil, nil,
				runtime.WithSnapshotSharedLister(newTestSharedLister(nil, []*v1.Node{tc.lowNode, tc.highNode})))
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
			for _, pod := range tc.lowNodePods {
				cache.addPod(tc.lowNode.Name, pod, time.Now())
			}
			for _, pod := range tc.highNodePods {
				cache.addPod(tc.highNode.Name, pod, time.Now())
			}

			lowScore, stat := p.scoreByPortrait(tc.pod, tc.lowNode.Name)
			assert.Nil(t, stat)
			assert.NotZero(t, lowScore)

			highScore, stat := p.scoreByPortrait(tc.pod, tc.highNode.Name)
			assert.Nil(t, stat)
			assert.NotZero(t, highScore)

			assert.Greater(t, highScore, lowScore)
		})
	}
}
