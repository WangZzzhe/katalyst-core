package types

import (
	"fmt"
	"strconv"
	"strings"
)

// Cell is the basic unit of the indicator
type Cell struct {
	Key string  `json:"key"`
	Val float64 `json:"val"`
}

type NodeMemoryResponse struct {
	Data []Cell `json:"data"`
}

type NodeCgroupMemoryResponse struct {
	Data []Cell `json:"data"`
}

type NUMAMemoryResponse struct {
	Data []Cell `json:"data"`
}

type CoreCPUResponse struct {
	Data []Cell `json:"data"`
}

type NodeSysctlResponse struct {
	Data []Cell `json:"data"`
}

type ContainerResponse struct {
	Data map[string][]Cell `json:"data"`
}

// key: numastat_{nodeNum}_{metricName}, eg, numastat_node0_memtotal
// return numaNode, metricName, error
func ParseNumastatKey(key string) (int, string, error) {
	data := strings.SplitN(key, "_", 3)
	if len(data) != 3 {
		return 0, "", fmt.Errorf("unknow numastat key %s", key)
	}

	if len(data[1]) < 5 {
		return 0, "", fmt.Errorf("unknow numastat key %s", key)
	}
	numaNode := data[1][4:len(data[1])]
	numaNodeNum, err := strconv.Atoi(numaNode)
	if err != nil {
		return 0, "", fmt.Errorf("unknow numastat key %s, numaNode %s", key, numaNode)
	}

	return numaNodeNum, data[2], nil
}

func ParseCorestatKey(key string) (int, string, error) {
	data := strings.SplitN(key, "_", 3)
	if len(data) != 3 {
		return 0, "", fmt.Errorf("unknow cpustat key %s", key)
	}

	if len(data[1]) < 4 {
		return 0, "", fmt.Errorf("unknow cpustat key %s", key)
	}
	cpu := data[1][3:len(data[1])]
	cpuNum, err := strconv.Atoi(cpu)
	if err != nil {
		return 0, "", fmt.Errorf("unknow cpustat key %s, cpu %s", key, cpu)
	}

	return cpuNum, data[2], nil
}
