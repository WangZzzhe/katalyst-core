package inspector

import (
	"context"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-core/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric/basic"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric/inspector/client"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric/inspector/types"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/pod"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	"github.com/kubewharf/katalyst-core/pkg/util/cgroup/common"
	utilmetric "github.com/kubewharf/katalyst-core/pkg/util/metric"
)

const (
	pageShift = 12
)

type InspectorMetricsFetcher struct {
	*basic.Fetcher

	client *client.InspectorClient

	podFetcher pod.PodFetcher

	emitter metrics.MetricEmitter

	synced bool
}

// NewInspectorMetricsFetcher returns the fetcher that fetch metrics by Inspector client
func NewInspectorMetricsFetcher(emitter metrics.MetricEmitter, fetcher pod.PodFetcher) metric.MetricsFetcher {
	return &InspectorMetricsFetcher{
		podFetcher: fetcher,
		client:     client.NewInspectorClient(fetcher, nil),
		Fetcher:    basic.NewFetcher(),
		emitter:    emitter,
		synced:     false,
	}
}

func (i *InspectorMetricsFetcher) Run(ctx context.Context) {
	go wait.Until(func() {
		i.sample(ctx)
	}, 5*time.Second, ctx.Done())
}

func (i *InspectorMetricsFetcher) sample(ctx context.Context) {
	i.updateNodeStats()

	i.updateNUMAStats()

	i.updateNodeCgroupStats()

	i.updateNodeSysctlStats()

	i.updateCoreStats()

	i.updatePodStats(ctx)

	// fetch external metric by registered metric functions
	i.UpdateRegisteredMetric()

	// notify registered notifier
	i.NotifySystem()
	i.NotifyPods()

	i.synced = true
}

func (i *InspectorMetricsFetcher) HasSynced() bool {
	return i.synced
}

func (i *InspectorMetricsFetcher) updateNodeStats() {

	// update node memory stats
	nodeMemoryData, err := i.client.GetNodeMemoryStats()
	if err != nil {
		klog.Errorf("[inspector] get node memory stats failed, err: %v", err)
	} else {
		i.processNodeMemoryData(nodeMemoryData)
	}
}

// updateNodeCgroupStats update only besteffort and burstable QoS level cgroup stats
func (i *InspectorMetricsFetcher) updateNodeCgroupStats() {

	// update cgroup memory stats
	memoryCgroupData, err := i.client.GetNodeCgroupMemoryStats()
	if err != nil {
		klog.Errorf("[inspector] get memory cgroup stats failed, err: %v", err)
	} else {
		i.processCgroupMemoryData(memoryCgroupData)
	}
}

func (i *InspectorMetricsFetcher) updateNodeSysctlStats() {

	// update node sysctl data
	sysctlData, err := i.client.GetNodeSysctl()
	if err != nil {
		klog.Errorf("[inspector] get node sysctl failed, err: %v", err)
	} else {
		i.processNodeSysctlData(sysctlData)
	}
}

func (i *InspectorMetricsFetcher) updateNUMAStats() {

	// update NUMA memory stats
	NUMAMemoryData, err := i.client.GetNUMAMemoryStats()
	if err != nil {
		klog.Errorf("[inspector] get NUMA memory stats failed, err: %v", err)
	} else {
		i.processNUMAMemoryData(NUMAMemoryData)
	}
}

func (i *InspectorMetricsFetcher) updateCoreStats() {

	// update core CPU stats
	coreCPUData, err := i.client.GetCoreCPUStats()
	if err != nil {
		klog.Errorf("[inspector] get core CPU stats failed, err: %v", err)
	} else {
		i.processCoreCPUData(coreCPUData)
	}
}

func (i *InspectorMetricsFetcher) updatePodStats(ctx context.Context) {
	// list all pods
	pods, err := i.podFetcher.GetPodList(ctx, func(_ *v1.Pod) bool { return true })
	if err != nil {
		klog.Errorf("[inspector] GetPodList fail: %v", err)
		return
	}

	podUIDSet := make(map[string]bool)
	for _, pod := range pods {
		podUIDSet[string(pod.UID)] = true
		cpuStats, err := i.client.GetPodContainerCPUStats(ctx, string(pod.UID))
		if err != nil {
			klog.Errorf("[inspector] get container CPU stats failed, pod: %v, err: %v", pod.Name, err)
		} else {
			for containerName, containerCPUStats := range cpuStats {
				i.processContainerCPUData(string(pod.UID), containerName, containerCPUStats)
			}
		}

		cgroupMemStats, err := i.client.GetPodContainerCgroupMemStats(ctx, string(pod.UID))
		if err != nil {
			klog.Errorf("[inspector] get container cgroupmem stats failed, pod: %v, err: %v", pod.Name, err)
		} else {
			for containerName, containerCgroupMem := range cgroupMemStats {
				i.processContainerCgroupMemData(string(pod.UID), containerName, containerCgroupMem)
			}
		}

		loadStats, err := i.client.GetPodContainerLoadStats(ctx, string(pod.UID))
		if err != nil {
			klog.Errorf("[inspector] get container load stats failed, pod: %v, err: %v", pod.Name, err)
		} else {
			for containerName, containerLoad := range loadStats {
				i.processContainerLoadData(string(pod.UID), containerName, containerLoad)
			}
		}

		cghardware, err := i.client.GetPodContainerCghardwareStats(ctx, string(pod.UID))
		if err != nil {
			klog.Errorf("[inspector] get container cghardware failed, pod: %v, err: %v", pod.Name, err)
		} else {
			for containerName, containerCghardware := range cghardware {
				i.processContainerCghardwareData(string(pod.UID), containerName, containerCghardware)
			}
		}

		cgNumaStats, err := i.client.GetPodContainerCgNumaStats(ctx, string(pod.UID))
		if err != nil {
			klog.Errorf("[inspector] get container numa stats failed, pod: %v, err: %v", pod.Name, err)
		} else {
			for containerName, containerNumaStats := range cgNumaStats {
				i.processContainerNumaData(string(pod.UID), containerName, containerNumaStats)
			}
		}
	}
	i.MetricStore.GCPodsMetric(podUIDSet)
}

func (i *InspectorMetricsFetcher) processNodeMemoryData(nodeMemoryData []types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.NodeMemoryPath]

	for _, cell := range nodeMemoryData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		i.MetricStore.SetNodeMetric(
			metricName,
			utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
		)

	}
}

func (i *InspectorMetricsFetcher) processNodeSysctlData(nodeSysctlData []types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.NodeSysctlPath]

	for _, cell := range nodeSysctlData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		i.MetricStore.SetNodeMetric(
			metricName,
			utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
		)

	}
}

func (i *InspectorMetricsFetcher) processCgroupMemoryData(cgroupMemoryData []types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.NodeCgroupMemoryPath]
	for _, cell := range cgroupMemoryData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		switch cell.Key {
		case "qosgroupmem_besteffort_memory_rss", "qosgroupmem_besteffort_memory_usage":
			i.MetricStore.SetCgroupMetric(common.CgroupFsRootPathBestEffort, metricName,
				utilmetric.MetricData{Value: cell.Val, Time: &updateTime})
		case "qosgroupmem_burstable_memory_rss", "qosgroupmem_burstable_memory_usage":
			i.MetricStore.SetCgroupMetric(common.CgroupFsRootPathBurstable, metricName,
				utilmetric.MetricData{Value: cell.Val, Time: &updateTime})
		}
	}
}

func (i *InspectorMetricsFetcher) processNUMAMemoryData(NUMAMemoryData map[int][]types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.NumaMemoryPath]

	for numaID, cells := range NUMAMemoryData {
		for _, cell := range cells {
			metricName, ok := metricMap[cell.Key]
			if !ok {
				continue
			}

			switch cell.Key {
			case "memtotal", "memfree":
				i.MetricStore.SetNumaMetric(
					numaID,
					metricName,
					utilmetric.MetricData{Value: float64(int(cell.Val) << 10), Time: &updateTime},
				)
			default:
				i.MetricStore.SetNumaMetric(
					numaID,
					metricName,
					utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
				)
			}

		}
	}
}

func (i *InspectorMetricsFetcher) processCoreCPUData(coreCPUData map[int][]types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.NodeCorePath]

	for cpuID, coreData := range coreCPUData {
		for _, cell := range coreData {
			metricName, ok := metricMap[cell.Key]
			if !ok {
				continue
			}

			switch cell.Key {
			case "usage":
				i.MetricStore.SetCPUMetric(
					cpuID,
					consts.MetricCPUUsageRatio,
					utilmetric.MetricData{Value: cell.Val / 100.0, Time: &updateTime},
				)
			case "sched_wait":
				i.MetricStore.SetCPUMetric(
					cpuID,
					consts.MetricCPUSchedwait,
					utilmetric.MetricData{Value: cell.Val * 1000, Time: &updateTime},
				)
			default:
				i.MetricStore.SetCPUMetric(
					cpuID,
					metricName,
					utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
				)
			}
		}
	}
}

func (i *InspectorMetricsFetcher) processContainerCPUData(podUID, containerName string, cpuData []types.Cell) {
	var (
		updateTime = time.Now()
		metricMap  = types.MetricsMap[types.ContainerCPUPath]
	)

	for _, cell := range cpuData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		switch cell.Key {
		case "cgcpu_usage":
			i.MetricStore.SetContainerMetric(
				podUID,
				containerName,
				metricName,
				utilmetric.MetricData{Value: cell.Val / 100.0, Time: &updateTime},
			)
		default:
			i.MetricStore.SetContainerMetric(
				podUID,
				containerName,
				metricName,
				utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
			)
		}
	}
}

func (i *InspectorMetricsFetcher) processContainerCghardwareData(podUID, containerName string, cghardwareData []types.Cell) {
	var (
		updateTime = time.Now()
		metricMap  = types.MetricsMap[types.ContainerCghardwarePath]

		cyclesOld, _         = i.GetContainerMetric(podUID, containerName, consts.MetricCPUCyclesContainer)
		instructionsOld, _   = i.GetContainerMetric(podUID, containerName, consts.MetricCPUInstructionsContainer)
		cycles, instructions float64
	)

	for _, cell := range cghardwareData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		i.MetricStore.SetContainerMetric(
			podUID,
			containerName,
			metricName,
			utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
		)

		if cell.Key == "cycles" {
			cycles = cell.Val
		}
		if cell.Key == "instructions" {
			instructions = cell.Val
		}
	}
	if cyclesOld.Value > 0 && cycles > 0 && instructionsOld.Value > 0 && instructions > 0 {
		instructionDiff := float64(instructions) - instructionsOld.Value
		if instructionDiff > 0 {
			cpi := (float64(cycles) - cyclesOld.Value) / instructionDiff
			i.MetricStore.SetContainerMetric(
				podUID,
				containerName,
				consts.MetricCPUCPIContainer,
				utilmetric.MetricData{Value: cpi, Time: &updateTime},
			)
		}
	}
}

func (i *InspectorMetricsFetcher) processContainerCgroupMemData(podUID, containerName string, cgroupMemData []types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.ContainerCgroupMemoryPath]

	for _, cell := range cgroupMemData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		i.MetricStore.SetContainerMetric(
			podUID,
			containerName,
			metricName,
			utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
		)
	}
}

func (i *InspectorMetricsFetcher) processContainerLoadData(podUID, containerName string, loadData []types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.ContainerLoadPath]

	for _, cell := range loadData {
		metricName, ok := metricMap[cell.Key]
		if !ok {
			continue
		}

		switch cell.Key {
		case "loadavg_loadavg1", "loadavg_loadavg5", "loadavg_loadavg15":
			i.MetricStore.SetContainerMetric(
				podUID,
				containerName,
				metricName,
				utilmetric.MetricData{Value: cell.Val / 100.0, Time: &updateTime},
			)
		default:
			i.MetricStore.SetContainerMetric(
				podUID,
				containerName,
				metricName,
				utilmetric.MetricData{Value: cell.Val, Time: &updateTime},
			)
		}

	}
}

func (i *InspectorMetricsFetcher) processContainerNumaData(podUID, containerName string, containerNumaData map[int][]types.Cell) {
	updateTime := time.Now()

	metricMap := types.MetricsMap[types.ContainerNumaStatPath]

	for numaNode, cells := range containerNumaData {
		for _, cell := range cells {
			metricName, ok := metricMap[cell.Key]
			if !ok {
				continue
			}

			switch cell.Key {
			case "filepage":
				i.MetricStore.SetContainerNumaMetric(podUID, containerName, strconv.Itoa(numaNode), metricName,
					utilmetric.MetricData{Value: float64(int(cell.Val) << pageShift), Time: &updateTime})
			default:

			}
		}
	}
}
