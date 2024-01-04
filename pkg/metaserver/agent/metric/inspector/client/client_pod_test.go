package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
)

func TestGetPodContainerCPUStats(t *testing.T) {
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "2126079c-8e0a-4cfe-9a0b-583199c14027",
			Name: "testPod",
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "testcontainer",
					ContainerID: "containerd://da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0",
				},
			},
		},
	}

	ic := NewInspectorClient(&FakePodFetcher{
		GetPodFunc: func(ctx context.Context, podUID string) (*v1.Pod, error) {
			return testPod, nil
		},
	}, func(url string, params map[string]string) ([]byte, error) {
		return []byte(`{"data":{"da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0":[{"key":"cgcpu_usage","val":99},{"key":"cgcpu_sysusage","val":0},{"key":"cgcpu_user_nsecs","val":2777495188589},{"key":"cgcpu_sys_nsecs","val":337561580},{"key":"cgcpu_userusage","val":99},{"key":"cgcpu_nsecs","val":2777832750169}]}}`), nil
	})

	res, err := ic.GetPodContainerCPUStats(context.Background(), "2126079c-8e0a-4cfe-9a0b-583199c14027")
	require.NoError(t, err)
	require.NotNil(t, res["testcontainer"])
}

func TestGetPodContainerCgroupMemStats(t *testing.T) {
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "2126079c-8e0a-4cfe-9a0b-583199c14027",
			Name: "testPod",
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "testcontainer",
					ContainerID: "containerd://da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0",
				},
			},
		},
	}

	ic := NewInspectorClient(&FakePodFetcher{
		GetPodFunc: func(ctx context.Context, podUID string) (*v1.Pod, error) {
			return testPod, nil
		},
	}, func(url string, params map[string]string) ([]byte, error) {
		return []byte(`{"data":{"da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0":[{"key":"cgmem_total_shmem","val":0},{"key":"cgmem_total_rss","val":0},{"key":"cgmem_total_cache","val":0},{"key":"cgmem_total_dirty","val":0}]}}`), nil
	})

	res, err := ic.GetPodContainerCgroupMemStats(context.Background(), "2126079c-8e0a-4cfe-9a0b-583199c14027")
	require.NoError(t, err)
	require.NotNil(t, res["testcontainer"])
}

func TestGetPodContainerLoadStats(t *testing.T) {
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "2126079c-8e0a-4cfe-9a0b-583199c14027",
			Name: "testPod",
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "testcontainer",
					ContainerID: "containerd://da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0",
				},
			},
		},
	}

	ic := NewInspectorClient(&FakePodFetcher{
		GetPodFunc: func(ctx context.Context, podUID string) (*v1.Pod, error) {
			return testPod, nil
		},
	}, func(url string, params map[string]string) ([]byte, error) {
		return []byte(`{"data":{"da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0":[{"key":"loadavg_loadavg15","val":48},{"key":"loadavg_loadavg5","val":48},{"key":"loadavg_nrrunning","val":48},{"key":"loadavg_loadavg1","val":48},{"key":"loadavg_nriowait","val":48},{"key":"loadavg_nrsleeping","val":48},{"key":"loadavg_nruninterruptible","val":48}]}}`), nil
	})

	res, err := ic.GetPodContainerLoadStats(context.Background(), "2126079c-8e0a-4cfe-9a0b-583199c14027")
	require.NoError(t, err)
	require.NotNil(t, res["testcontainer"])
}

func TestGetPodContainerCghardwareStats(t *testing.T) {
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "2126079c-8e0a-4cfe-9a0b-583199c14027",
			Name: "testPod",
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "testcontainer",
					ContainerID: "containerd://da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0",
				},
			},
		},
	}

	ic := NewInspectorClient(&FakePodFetcher{
		GetPodFunc: func(ctx context.Context, podUID string) (*v1.Pod, error) {
			return testPod, nil
		},
	}, func(url string, params map[string]string) ([]byte, error) {
		return []byte(`{"data":{"da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0":[{"key":"cghardware_cycles","val":0},{"key":"cghardware_instrcutions","val":0}]}}`), nil
	})

	res, err := ic.GetPodContainerCghardwareStats(context.Background(), "2126079c-8e0a-4cfe-9a0b-583199c14027")
	require.NoError(t, err)
	require.NotNil(t, res["testcontainer"])
}

func TestGetPodContainerCgNumaStats(t *testing.T) {
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "2126079c-8e0a-4cfe-9a0b-583199c14027",
			Name: "testPod",
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "testcontainer",
					ContainerID: "containerd://da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0",
				},
			},
		},
	}

	ic := NewInspectorClient(&FakePodFetcher{
		GetPodFunc: func(ctx context.Context, podUID string) (*v1.Pod, error) {
			return testPod, nil
		},
	}, func(url string, params map[string]string) ([]byte, error) {
		return []byte(`{"data":{"da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0":[{"key":"cgnumastat_filepage","val":5},{"key":"cgnumastat_node0_filepage","val":5}]}}`), nil
	})

	res, err := ic.GetPodContainerCgNumaStats(context.Background(), "2126079c-8e0a-4cfe-9a0b-583199c14027")
	require.NoError(t, err)
	require.NotNil(t, res["testcontainer"])
	require.NotNil(t, res["testcontainer"][0])
	require.Equal(t, 1, len(res["testcontainer"]))
}

func TestGetMetrics(t *testing.T) {
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:  "2126079c-8e0a-4cfe-9a0b-583199c14027",
			Name: "testPod",
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "testcontainer",
					ContainerID: "containerd://da2f6c58a278f9966c2951f3297ed2ac6b8c73cc92460e68f6865bc0448137f0",
				},
			},
		},
	}

	ic := NewInspectorClient(&FakePodFetcher{
		GetPodFunc: func(ctx context.Context, podUID string) (*v1.Pod, error) {
			return testPod, nil
		},
	}, nil)

	res, err := ic.GetPodContainerCgNumaStats(context.Background(), "2126079c-8e0a-4cfe-9a0b-583199c14027")
	require.Error(t, err)
	require.Nil(t, res)
}
