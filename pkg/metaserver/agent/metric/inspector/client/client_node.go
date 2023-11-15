package client

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric/inspector/types"
)

func (c *InspectorClient) GetNodeMemoryStats() ([]types.Cell, error) {
	url := c.urls[types.NodeMemoryPath]

	data, err := c.metricFunc(url, nil)
	if err != nil {
		klog.Errorf("GetNodeMemoryStats fail, err: %v", err)
		return nil, err
	}

	rsp := &types.NodeMemoryResponse{}
	if err = json.Unmarshal(data, rsp); err != nil {
		klog.Errorf("faild to unmarshal node memory response, err: %v, data: %v", err, string(data))
		return nil, err
	}

	if len(rsp.Data) == 0 {
		err = fmt.Errorf("GetNodeMemoryStats fail, empty data")
		klog.Error(err)
		return nil, err
	}

	return rsp.Data, nil
}

func (c *InspectorClient) GetNodeCgroupMemoryStats() ([]types.Cell, error) {
	url := c.urls[types.NodeCgroupMemoryPath]

	data, err := c.metricFunc(url, nil)
	if err != nil {
		klog.Errorf("GetNodeNUMAMemoryStatus fail, err: %v", err)
		return nil, err
	}

	rsp := &types.NodeCgroupMemoryResponse{}
	if err = json.Unmarshal(data, rsp); err != nil {
		klog.Errorf("faild to unmarshal node cgroup memory response, err: %v, data: %v", err, string(data))
		return nil, err
	}

	return rsp.Data, err
}

func (c *InspectorClient) GetNUMAMemoryStats() (map[int][]types.Cell, error) {
	url := c.urls[types.NumaMemoryPath]

	data, err := c.metricFunc(url, nil)
	if err != nil {
		klog.Errorf("GetNUMAMemoryStats fail, err: %v", err)
		return nil, err
	}

	rsp := &types.NUMAMemoryResponse{}
	if err = json.Unmarshal(data, rsp); err != nil {
		err = fmt.Errorf("failed to unmarshal numa memory response, err: %v, data: %v", err, string(data))
		return nil, err
	}

	res := make(map[int][]types.Cell)
	for _, cell := range rsp.Data {
		numaNode, metric, err := types.ParseNumastatKey(cell.Key)
		if err != nil {
			return nil, err
		}

		if _, ok := res[numaNode]; !ok {
			res[numaNode] = make([]types.Cell, 0)
		}
		res[numaNode] = append(res[numaNode], types.Cell{
			Key: metric,
			Val: cell.Val,
		})
	}

	return res, err
}

func (c *InspectorClient) GetCoreCPUStats() (map[int][]types.Cell, error) {
	url := c.urls[types.NodeCorePath]

	data, err := c.metricFunc(url, nil)
	if err != nil {
		klog.Errorf("GetCoreCPUStats fail, err: %v", err)
		return nil, err
	}

	rsp := &types.CoreCPUResponse{}
	if err = json.Unmarshal(data, rsp); err != nil {
		err = fmt.Errorf("failed to unmarshal core cpu response, err: %v, data: %v", err, string(data))
		return nil, err
	}

	res := make(map[int][]types.Cell)
	for _, cell := range rsp.Data {
		cpu, metric, err := types.ParseCorestatKey(cell.Key)
		if err != nil {
			return nil, err
		}

		if _, ok := res[cpu]; !ok {
			res[cpu] = make([]types.Cell, 0)
		}
		res[cpu] = append(res[cpu], types.Cell{
			Key: metric,
			Val: cell.Val,
		})
	}

	return res, err
}

func (c *InspectorClient) GetNodeSysctl() ([]types.Cell, error) {
	url := c.urls[types.NodeSysctlPath]

	data, err := c.metricFunc(url, nil)
	if err != nil {
		klog.Errorf("GetNodeSysctl fail, err: %v", err)
		return nil, err
	}

	rsp := &types.NodeSysctlResponse{}
	if err = json.Unmarshal(data, rsp); err != nil {
		err = fmt.Errorf("failed to unmarshal node sysctl response, err: %v, data: %v", err, string(data))
		return nil, err
	}

	if len(rsp.Data) == 0 {
		err = fmt.Errorf("GetNodeSysctl fail, empty data")
		klog.Error(err)
		return nil, err
	}

	return rsp.Data, nil
}
