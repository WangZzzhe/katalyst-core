package loadaware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAddPod(t *testing.T) {
	t.Parallel()

	c := &Cache{
		NodePodInfo: map[string]*NodeCache{},
	}

	c.addPod("testNode", &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "testPod",
			UID:  "testPod",
		},
	}, time.Now())
	assert.Equal(t, 1, len(c.NodePodInfo["testNode"].PodInfoMap))

	c.addPod("testNode", nil, time.Now())
	assert.Equal(t, 1, len(c.NodePodInfo["testNode"].PodInfoMap))

	c.addPod("testNode", &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "testPod",
			UID:  "testPod",
		},
	}, time.Now())
	assert.Equal(t, 1, len(c.NodePodInfo["testNode"].PodInfoMap))

	c.addPod("testNode", &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "testPod2",
			UID:  "testPod2",
		},
	}, time.Now())
	assert.Equal(t, 2, len(c.NodePodInfo["testNode"].PodInfoMap))

	c.addPod("testNode2", &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "testPod2",
			UID:  "testPod2",
		},
	}, time.Now())
	assert.Equal(t, 2, len(c.NodePodInfo))

	c.removePod("testNode2", &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "testPod2",
			UID:  "testPod2",
		},
	})
	assert.Equal(t, 1, len(c.NodePodInfo))

	c.removePod("testNode", &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: "testPod2",
			UID:  "testPod2",
		}})
	assert.Equal(t, 1, len(c.NodePodInfo["testNode"].PodInfoMap))
}
