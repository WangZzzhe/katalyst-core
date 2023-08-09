package matcher

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"reflect"
	"testing"

	"github.com/kubewharf/katalyst-api/pkg/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type FakeNodeIndexer struct {
	nodeMap map[string]*v1.Node
}

func newFakeNodeIndexer() *FakeNodeIndexer {
	return &FakeNodeIndexer{
		nodeMap: make(map[string]*v1.Node),
	}
}

func (fn *FakeNodeIndexer) GetNode(nodeName string) (*v1.Node, error) {
	if _, ok := fn.nodeMap[nodeName]; !ok {
		return nil, errors.NewNotFound(v1.Resource("Node"), nodeName)
	}
	return fn.nodeMap[nodeName], nil
}

func (fn *FakeNodeIndexer) ListNode(selector labels.Selector) ([]*v1.Node, error) {
	nodeList := make([]*v1.Node, 0)
	for _, node := range fn.nodeMap {
		if selector.Matches(labels.Set(node.Labels)) {
			nodeList = append(nodeList, node)
		}
	}
	return nodeList, nil
}

func (fn *FakeNodeIndexer) add(node *v1.Node) {
	fn.nodeMap[node.Name] = node
}

type FakeNocIndexer struct {
	configMap map[string]*v1alpha1.NodeOvercommitConfig
}

func newFakeNocIndexer() *FakeNocIndexer {
	return &FakeNocIndexer{
		configMap: make(map[string]*v1alpha1.NodeOvercommitConfig),
	}
}

func (fn *FakeNocIndexer) GetNoc(name string) (*v1alpha1.NodeOvercommitConfig, error) {
	if _, ok := fn.configMap[name]; !ok {
		return nil, errors.NewNotFound(v1.Resource("NodeOvercommitConfig"), name)
	}
	return fn.configMap[name], nil
}

func (fn *FakeNocIndexer) ListNoc() ([]*v1alpha1.NodeOvercommitConfig, error) {
	configList := make([]*v1alpha1.NodeOvercommitConfig, 0)
	for _, c := range fn.configMap {
		configList = append(configList, c)
	}
	return configList, nil
}

func (fn *FakeNocIndexer) add(config *v1alpha1.NodeOvercommitConfig) {
	fn.configMap[config.Name] = config
}

func makeTestMatcher() *MatcherImpl {
	nodeIndexer := makeTestNodeIndexer()
	nocIndexer := makeTestNocIndexer()

	return NewMatcher(nodeIndexer, nocIndexer)
}

func makeInitedMatcher() (*MatcherImpl, error) {
	m := makeTestMatcher()
	_, err := m.Reconcile()
	return m, err
}

func makeTestNodeIndexer() *FakeNodeIndexer {
	nodeIndexer := newFakeNodeIndexer()

	nodeIndexer.add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", UID: "01", Labels: map[string]string{"pool": "pool1"}}})
	nodeIndexer.add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2", UID: "02", Labels: map[string]string{"pool": "pool2"}}})
	nodeIndexer.add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3", UID: "03", Labels: map[string]string{"pool": "pool1"}}})
	nodeIndexer.add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node4", UID: "04", Labels: map[string]string{"pool": "pool3"}}})
	return nodeIndexer
}

func makeTestNocIndexer() *FakeNocIndexer {
	nocIndexer := newFakeNocIndexer()

	nocIndexer.add(&v1alpha1.NodeOvercommitConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config1-nodeList",
		},
		Spec: v1alpha1.NodeOvercommitConfigSpec{
			NodeList: []string{
				"node1",
				"node2",
			},
			ResourceOvercommitRatioConfig: map[v1.ResourceName]string{
				v1.ResourceCPU:    "1.5",
				v1.ResourceMemory: "1",
			},
		},
	})

	nocIndexer.add(&v1alpha1.NodeOvercommitConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config2-default",
		},
		Spec: v1alpha1.NodeOvercommitConfigSpec{
			ResourceOvercommitRatioConfig: map[v1.ResourceName]string{
				v1.ResourceCPU:    "1",
				v1.ResourceMemory: "1",
			},
		},
	})

	nocIndexer.add(&v1alpha1.NodeOvercommitConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config3-selector",
		},
		Spec: v1alpha1.NodeOvercommitConfigSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"pool": "pool1",
				},
			},
			ResourceOvercommitRatioConfig: map[v1.ResourceName]string{
				v1.ResourceCPU: "2",
			},
		},
	})

	return nocIndexer
}

func makeMatcherByIndexer(nodeIndexer *FakeNodeIndexer, nocIndexer *FakeNocIndexer) *MatcherImpl {
	matcher := NewMatcher(nodeIndexer, nocIndexer)
	_, _ = matcher.Reconcile()
	return matcher
}

func TestMatchConfigNameToNodes(t *testing.T) {

	testCases := []struct {
		name       string
		configName string
		result     []string
	}{
		{
			name:       "nodeList",
			configName: "config1-nodeList",
			result:     []string{"node1", "node2"},
		},
		{
			name:       "selector",
			configName: "config3-selector",
			result:     []string{"node1", "node3"},
		},
		{
			name:       "default matches all nodes",
			configName: "config2-default",
			result:     []string{"node1", "node2", "node3", "node4"},
		},
	}

	matcher := makeTestMatcher()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := matcher.matchConfigNameToNodes(tc.configName)
			assert.Nil(t, err)
			assert.True(t, reflect.DeepEqual(out, tc.result))
		})
	}
}

func TestReconcile(t *testing.T) {
	t.Parallel()

	matcher1 := makeTestMatcher()

	matcher2, _ := makeInitedMatcher()
	matcher2.nodeToConfig["node3"] = "config2-default"
	delete(matcher2.nodeToConfig, "node4")

	testCases := []struct {
		name          string
		matcher       Matcher
		fixedNodeList []string
	}{
		{
			name:          "init",
			matcher:       matcher1,
			fixedNodeList: []string{"node1", "node2", "node3", "node4"},
		},
		{
			name:          "reconcile",
			matcher:       matcher2,
			fixedNodeList: []string{"node3", "node4"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.matcher.Reconcile()
			assert.Nil(t, err)
			assert.True(t, reflect.DeepEqual(out, tc.fixedNodeList))
		})
	}
}

func TestMatchConfig(t *testing.T) {
	nodeIndexer1 := makeTestNodeIndexer()
	nocIndexer1 := makeTestNocIndexer()
	matcher1 := makeMatcherByIndexer(nodeIndexer1, nocIndexer1)
	nocIndexer1.configMap["config3-selector"].Spec.Selector.MatchLabels["pool"] = "pool2"

	nodeIndexer2 := makeTestNodeIndexer()
	nocIndexer2 := makeTestNocIndexer()
	matcher2 := makeMatcherByIndexer(nodeIndexer2, nocIndexer2)
	nocIndexer2.add(&v1alpha1.NodeOvercommitConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config4-selector",
		},
		Spec: v1alpha1.NodeOvercommitConfigSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"pool": "pool2",
				},
			},
			ResourceOvercommitRatioConfig: map[v1.ResourceName]string{
				v1.ResourceCPU: "3",
			},
		},
	})

	nodeIndexer3 := makeTestNodeIndexer()
	nocIndexer3 := makeTestNocIndexer()
	matcher3 := makeMatcherByIndexer(nodeIndexer3, nocIndexer3)
	delete(nocIndexer3.configMap, "config3-selector")

	nodeIndexer4 := makeTestNodeIndexer()
	nocIndexer4 := makeTestNocIndexer()
	matcher4 := makeMatcherByIndexer(nodeIndexer4, nocIndexer4)
	nocIndexer4.configMap["config1-nodeList"].Spec.NodeList = nil
	nocIndexer4.configMap["config1-nodeList"].Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"pool": "pool2",
		},
	}

	testCases := []struct {
		name       string
		matcher    Matcher
		configName string
		result     []string
	}{
		{
			name:       "update selector",
			matcher:    matcher1,
			configName: "config3-selector",
			result:     []string{"node1", "node2", "node3"},
		},
		{
			name:       "add config",
			matcher:    matcher2,
			configName: "config4-selector",
			result:     []string{"node2"},
		},
		{
			name:       "delete config",
			matcher:    matcher3,
			configName: "config3-selector",
			result:     []string{"node1", "node3"},
		},
		{
			name:       "update config type",
			matcher:    matcher4,
			configName: "config1-nodeList",
			result:     []string{"node1", "node2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.matcher.MatchConfig(tc.configName)
			assert.Nil(t, err)
			assert.Equal(t, len(out), len(tc.result))
		})
	}
}

func TestMatchNode(t *testing.T) {

}
