package loadaware

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReserve(t *testing.T) {
	t.Parallel()

	p := &Plugin{}
	p.Reserve(context.TODO(), nil, &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testPod",
			UID:  "testPod",
		},
	}, "testNode")
	assert.Equal(t, 1, len(cache.NodePodInfo))
	assert.Equal(t, 1, len(cache.NodePodInfo["testNode"].PodInfoMap))

	p.Unreserve(context.TODO(), nil, &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testPod",
			UID:  "testPod",
		},
	}, "testNode")
	assert.Equal(t, 0, len(cache.NodePodInfo))
}
