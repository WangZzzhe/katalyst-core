package types

import "github.com/kubewharf/katalyst-core/pkg/consts"

const (
	InspectorServerPort = 9102

	NodeMemoryPath       = "/node/memory"
	NodeCgroupMemoryPath = "/node/cgroupmem"

	NumaMemoryPath = "/node/numastat"

	NodeCorePath = "/node/percorecpu"

	NodeSysctlPath = "/node/sysctl"

	ContainerCPUPath          = "/container/cgcpu"
	ContainerCgroupMemoryPath = "/container/cgmem"
	ContainerNumaStatPath     = "/container/cgnumastat"
	ContainerLoadPath         = "/container/loadavg"
	ContainerCghardwarePath   = "/container/cghardware"
)

const (
	Name = "inspector"
)

// read only
var MetricsMap = map[string]map[string]string{
	NodeMemoryPath: {
		"memory_memtotal":       consts.MetricMemTotalSystem,
		"memory_memfree":        consts.MetricMemFreeSystem,
		"memory_memused":        consts.MetricMemUsedSystem,
		"memory_cached":         consts.MetricMemPageCacheSystem,
		"memory_buffers":        consts.MetricMemBufferSystem,
		"memory_pgsteal_kswapd": consts.MetricMemKswapdstealSystem,
	},
	NodeCgroupMemoryPath: {
		"qosgroupmem_besteffort_memory_rss":   consts.MetricMemRssCgroup,
		"qosgroupmem_besteffort_memory_usage": consts.MetricMemUsageCgroup,
		"qosgroupmem_burstable_memory_rss":    consts.MetricMemRssCgroup,
		"qosgroupmem_burstable_memory_usage":  consts.MetricMemUsageCgroup,
	},
	NumaMemoryPath: {
		"memtotal": consts.MetricMemTotalNuma,
		"memfree":  consts.MetricMemFreeNuma,
	},
	NodeCorePath: {
		"usage":      consts.MetricCPUUsageRatio,
		"sched_wait": consts.MetricCPUSchedwait,
	},
	NodeSysctlPath: {
		"sysctl_vm_watermark_scale_factor": consts.MetricMemScaleFactorSystem,
	},
	ContainerCPUPath: {
		"cgcpu_usage": consts.MetricCPUUsageContainer,
	},
	ContainerCgroupMemoryPath: {
		"cgmem_total_shmem": consts.MetricMemShmemContainer,
		"cgmem_total_rss":   consts.MetricMemRssContainer,
		"cgmem_total_cache": consts.MetricMemCacheContainer,
	},
	ContainerLoadPath: {
		"loadavg_nrrunning": consts.MetricCPUNrRunnableContainer,
		"loadavg_loadavg1":  consts.MetricLoad1MinContainer,
		"loadavg_loadavg5":  consts.MetricLoad5MinContainer,
		"loadavg_loadavg15": consts.MetricLoad15MinContainer,
	},
	ContainerNumaStatPath: {
		"filepage": consts.MetricsMemFilePerNumaContainer,
	},
	ContainerCghardwarePath: {
		"cghardware_cycles":       consts.MetricCPUCyclesContainer,
		"cghardware_instrcutions": consts.MetricCPUInstructionsContainer,
	},
}
