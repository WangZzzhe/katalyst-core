package client

import (
	"fmt"
	"io"
	"net/http"

	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric/inspector/types"

	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/pod"
)

type InspectorClient struct {
	urls map[string]string

	fetcher pod.PodFetcher

	metricFunc MetricFunc
}

func NewInspectorClient(fetcher pod.PodFetcher, metricFunc MetricFunc) *InspectorClient {
	urls := make(map[string]string)
	for path := range types.MetricsMap {
		urls[path] = fmt.Sprintf("http://localhost:%d%s", types.InspectorServerPort, path)
	}

	if metricFunc == nil {
		metricFunc = getMetrics
	}

	return &InspectorClient{
		fetcher:    fetcher,
		urls:       urls,
		metricFunc: metricFunc,
	}
}

type MetricFunc func(url string, params map[string]string) ([]byte, error)

func getMetrics(url string, params map[string]string) ([]byte, error) {
	if len(params) != 0 {
		firstParam := true

		for k, v := range params {
			if firstParam {
				url += "?"
				firstParam = false
			} else {
				url += "&"
			}
			url = fmt.Sprintf("%s%s=%s", url, k, v)
		}
	}

	rsp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics, url: %v, err: %v", url, err)
	}
	defer func() { _ = rsp.Body.Close() }()

	if rsp.StatusCode != 200 {
		return nil, fmt.Errorf("invalid http response status code %d, status: %s, url: %s", rsp.StatusCode, rsp.Status, url)
	}

	return io.ReadAll(rsp.Body)
}
