package strategy

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	pluginapi "github.com/kubewharf/katalyst-api/pkg/protocol/evictionplugin/v1alpha1"
	advisorapi "github.com/kubewharf/katalyst-core/pkg/agent/qrm-plugins/cpu/dynamicpolicy/cpuadvisor"
	"github.com/kubewharf/katalyst-core/pkg/agent/qrm-plugins/cpu/dynamicpolicy/state"
	"github.com/kubewharf/katalyst-core/pkg/config"
	"github.com/kubewharf/katalyst-core/pkg/config/agent/dynamic"
	"github.com/kubewharf/katalyst-core/pkg/config/generic"
	"github.com/kubewharf/katalyst-core/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/metaserver"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	cgroupcm "github.com/kubewharf/katalyst-core/pkg/util/cgroup/common"
	cgroupcmutils "github.com/kubewharf/katalyst-core/pkg/util/cgroup/manager"
	"github.com/kubewharf/katalyst-core/pkg/util/general"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

const (
	EvictionPluginNameSystemCPUPressure        = "system-cpu-pressure-eviction-plugin"
	EvictionScopeSystemCPU                     = "SystemCPU"
	EvictionConditionSystemCPUPressure         = "SystemCPUPressure"
	EvictionConditionSystemCPUPressureSuppress = "SystemCPUPressureSuppression"
)

var (
	handleUsageMetrics = sets.NewString(
		// cpu 用量
		consts.MetricCPUUsageRatio,
		consts.MetricCPUUsageContainer,
	)
	skipUsagePools = sets.NewString(
		//state.PoolNameReclaim,
		state.PoolNameDedicated,
		state.PoolNameFallback,
		state.PoolNameReserve,
	)
)

type SystemCPUPressureUsageEviction struct {
	sync.Mutex
	state       state.ReadonlyState
	emitter     metrics.MetricEmitter
	metaServer  *metaserver.MetaServer
	qosConf     *generic.QoSConfiguration
	dynamicConf *dynamic.DynamicAgentConfiguration

	// [poolName][metricName]Value
	metricCurrent map[string]map[string]float64

	metricsHistory            SubEntries
	poolMetricCollectHandlers map[string]PoolMetricCollectHandler

	conf         *config.Configuration
	systemAction int

	isPress                      bool
	suppressionCpuQuota          float64
	reclaimedPoolHigherCPUBuffer float64
	reclaimedPoolLowerCPUBuffer  float64

	syncPeriod       time.Duration
	lastEvictionTime time.Time

	isUnderSystemPressure bool
	evictionHelper        *EvictionHelper
}

func NewSystemCPUPressureUsageEviction(emitter metrics.MetricEmitter, metaServer *metaserver.MetaServer,
	conf *config.Configuration, state state.ReadonlyState) (CPUPressureEviction, error) {
	plugin := &SystemCPUPressureUsageEviction{
		state:          state,
		emitter:        emitter,
		metaServer:     metaServer,
		metricsHistory: make(SubEntries),
		conf:           conf,
		qosConf:        conf.QoSConfiguration,
		dynamicConf:    conf.DynamicAgentConfiguration,
		syncPeriod:     conf.UsageEvictionSyncPeriod,
		evictionHelper: NewEvictionHelper(emitter, metaServer, conf),
	}
	return plugin, nil
}

func (p *SystemCPUPressureUsageEviction) Start(ctx context.Context) (err error) {
	general.Infof("%s", p.Name())
	go wait.UntilWithContext(ctx, p.collectMetrics, p.syncPeriod)
	return
}

func (p *SystemCPUPressureUsageEviction) Name() string { return "system-pressure-usage" }

func (p *SystemCPUPressureUsageEviction) GetEvictPods(context.Context, *pluginapi.GetEvictPodsRequest) (*pluginapi.GetEvictPodsResponse, error) {
	return &pluginapi.GetEvictPodsResponse{}, nil
}

func (p *SystemCPUPressureUsageEviction) collectMetrics(_ context.Context) {
	general.Infof("execute")
	_ = p.emitter.StoreInt64(metricsNameCollectMetricsCalled, 1, metrics.MetricTypeNameRaw)

	p.Lock()
	defer p.Unlock()
	dynamicConfig := p.dynamicConf.GetDynamicConfiguration()
	general.Infof("EnableSystemUsageEviction: %v", dynamicConfig.EnableSystemUsageEviction)
	if !dynamicConfig.EnableSystemUsageEviction {
		p.metricsHistory = make(SubEntries)
		return
	}
	p.isUnderSystemPressure = false
	p.systemAction = actionNoop
	p.collectPodPoolMetric()
	p.detectSystemCPUPressure()
	return
}

func (p *SystemCPUPressureUsageEviction) detectSystemCPUPressure() {
	// 0.1571596
	nodeCpuUsageRatio, err := p.metaServer.GetNodeMetric(consts.MetricCPUUsageRatio)
	if err != nil {
		general.Errorf("failed to GetNodeMetricWithTime, err: %v", err)
		return
	}
	// 15
	nodeCpuUsagePercent := nodeCpuUsageRatio.Value * 100.0
	//dynamicConfig.SystemUsageThresholdMetPercentage 20
	dynamicConfig := p.dynamicConf.GetDynamicConfiguration()
	//general.Infof("GetNodeMetric, SystemUsageThresholdMetPercentage: %v", dynamicConfig.SystemUsageThresholdMetPercentage)
	//general.Infof("GetNodeMetric, nodeCpuUsagePercent: %v", nodeCpuUsagePercent)

	// 检查 reclaimed_pool 资源用量
	// 1.如果离线已经压制到 nodecpu * 5% ，直接驱逐。
	// 2.如果离线空余较大，进行压制。
	node, err := p.metaServer.GetNode(context.TODO())
	if err != nil {
		general.Errorf("metaServer.GetNode err: %v", err)
		return
	}

	// 离线池 cpu 用量
	reclaimedPoolCPUUsage := p.getMetricForPool(consts.MetricCPUUsageContainer, "reclaim")
	reclaimedPoolCPUUsageMillion := reclaimedPoolCPUUsage * 1000
	//general.Infof("detectSystemCPUPressure, reclaimedPoolCPUUsageMillion: %f", reclaimedPoolCPUUsageMillion)
	// 64000
	nodeCPUCapacity := node.Status.Capacity.Cpu().MilliValue()
	//general.Infof("detectSystemCPUPressure, nodeCPUCapacity: %d", nodeCPUCapacity)
	// 节点cpu水位线上限
	SystemUsageThresholdMetQuota := dynamicConfig.SystemUsageThresholdMetPercentage * float64(nodeCPUCapacity) / 100
	//general.Infof("detectSystemCPUPressure, SystemUsageThresholdMetQuota: %f", SystemUsageThresholdMetQuota)
	// 节点 cpu 用量
	nodeCpuUsage := float64(nodeCPUCapacity) * nodeCpuUsageRatio.Value
	//general.Infof("detectSystemCPUPressure, nodeCpuUsage: %f", nodeCpuUsage)
	// 在线池 cpu 用量
	onlinePoolCPUUsage := nodeCpuUsage - reclaimedPoolCPUUsageMillion
	//general.Infof("detectSystemCPUPressure, onlinePoolCPUUsage: %f", onlinePoolCPUUsage)
	// 离线 cpu 最大可用  水位线 - 在线
	reclaimedPoolHigherCPUBuffer := SystemUsageThresholdMetQuota - onlinePoolCPUUsage
	if reclaimedPoolHigherCPUBuffer < 0 {
		reclaimedPoolHigherCPUBuffer = 0
	}
	//general.Infof("detectSystemCPUPressure, reclaimedPoolHigherCPUBuffer: %f", reclaimedPoolHigherCPUBuffer)
	// 最低压制预留
	reclaimedPoolLowerCPUBuffer := float64(nodeCPUCapacity) / 20
	//general.Infof("detectSystemCPUPressure, reclaimedPoolLowerCPUBuffer: %f", reclaimedPoolLowerCPUBuffer)

	general.Infof("node metric, CPUCapacity: %d, nodeCpuUsage: %f, CPUUsagePercent: %f", nodeCPUCapacity, nodeCpuUsage, nodeCpuUsagePercent)
	general.Infof("node metric Threshold, SystemUsageThresholdMetPercentage: %f, SystemUsageThresholdMetQuota: %f", dynamicConfig.SystemUsageThresholdMetPercentage, SystemUsageThresholdMetQuota)
	general.Infof("node metric reclaimed-pool, reclaimedPoolCPUUsage: %f, onlinePoolCPUUsage: %f", reclaimedPoolCPUUsageMillion, onlinePoolCPUUsage)
	general.Infof("node metric reclaimed-pool buffer, reclaimedPoolHigherCPUBuffer: %f ,reclaimedPoolLowerCPUBuffer: %f", reclaimedPoolHigherCPUBuffer, reclaimedPoolLowerCPUBuffer)

	p.reclaimedPoolHigherCPUBuffer = reclaimedPoolHigherCPUBuffer
	p.reclaimedPoolLowerCPUBuffer = reclaimedPoolLowerCPUBuffer

	p.isPress = false
	// 如何节点使用率比当前节点水位小，则直接返回
	if nodeCpuUsagePercent < dynamicConfig.SystemUsageThresholdMetPercentage {
		collectTime := time.Now().UnixNano()
		snapshot := &MetricSnapshot{
			Info: MetricInfo{
				Name:    consts.MetricCPUUsageContainer,
				Value:   0,
				IsPress: false,
			},
			Time: collectTime,
		}
		// 组织按照 pool 存储数据
		general.Infof("111,poolsMetric: metricName: %s, IsPress: %v", consts.MetricCPUUsageContainer, false)
		p.pushMetric(dynamicConfig, consts.MetricCPUUsageContainer, snapshot)
		return
	}
	// 记录当前压力
	collectTime := time.Now().UnixNano()
	snapshot := &MetricSnapshot{
		Info: MetricInfo{
			Name:    consts.MetricCPUUsageContainer,
			Value:   0,
			IsPress: true,
		},
		Time: collectTime,
	}
	p.isPress = true
	// 组织按照 pool 存储数据
	general.Infof("111,poolsMetric: metricName: %s, IsPress: %v", consts.MetricCPUUsageContainer, true)
	p.pushMetric(dynamicConfig, consts.MetricCPUUsageContainer, snapshot)
}

func (p *SystemCPUPressureUsageEviction) collectPodPoolMetric() {
	pod2Pool := getPodPoolMapFunc(p.metaServer.MetaAgent, p.state)
	//p.clearExpiredMetricsHistory(pod2Pool)
	// collect metric for pod/container pairs, and store in local (i.e. poolsMetric)
	//collectTime := time.Now().UnixNano()
	poolsMetric := make(map[string]map[string]float64)
	//dynamicConfig := p.dynamicConf.GetDynamicConfiguration()
	for podUID, entry := range pod2Pool {
		for containerName, containerEntry := range entry {
			general.Infof("containerName: %s", containerName)
			if containerEntry == nil || containerEntry.IsPool {
				continue
			} else if containerEntry.OwnerPool == advisorapi.EmptyOwnerPoolName || skipUsagePools.Has(containerEntry.OwnerPool) {
				general.Infof("skip collecting metric for pod: %s, container: %s with owner pool name: %s",
					podUID, containerName, containerEntry.OwnerPool)
				continue
			}

			poolName := containerEntry.OwnerPool
			general.Infof("poolName: %s", poolName)
			for _, metricName := range handleUsageMetrics.UnsortedList() {
				m, err := p.metaServer.GetContainerMetric(podUID, containerName, metricName)
				if err != nil {
					general.Errorf("GetContainerMetric for pod: %s, container: %s failed with error: %v", podUID, containerName, err)
					continue
				}
				general.Infof("GetContainerMetric for pod: %s, container: %s value: %v", podUID, containerName, m.Value)
				//snapshot := &MetricSnapshot{
				//	Info: MetricInfo{
				//		Name:  metricName,
				//		Value: m.Value,
				//	},
				//	Time: collectTime,
				//}

				//p.pushMetric(dynamicConfig, metricName, podUID, containerName, snapshot)

				if poolsMetric[poolName] == nil {
					poolsMetric[poolName] = make(map[string]float64)
				}
				poolsMetric[poolName][metricName] += m.Value
				general.Infof("poolsMetric: poolName: %s, metricName: %s, value: %f", poolName, metricName, poolsMetric[poolName][metricName])
			}
		}
	}
	p.metricCurrent = poolsMetric
	//for poolName, metricEntry := range poolsMetric {
	//	if metricEntry == nil || skipUsagePools.Has(poolName) {
	//		continue
	//	}
	//
	//	for _, metricName := range handleUsageMetrics.UnsortedList() {
	//		if _, found := poolsMetric[poolName][metricName]; !found {
	//			continue
	//		}
	//		collectTime := time.Now().UnixNano()
	//		snapshot := &MetricSnapshot{
	//			Info: MetricInfo{
	//				Name:  metricName,
	//				Value: metricEntry[metricName],
	//			},
	//			Time: collectTime,
	//		}
	//		// 组织按照 pool 存储数据
	//		general.Infof("111,poolsMetric: poolName: %s, metricName: %s, value: %f", poolName, metricName, poolsMetric[poolName][metricName])
	//		p.pushMetric(dynamicConfig, metricName, poolName, advisorapi.FakedContainerName, snapshot)
	//
	//	}
	//}
	return
}

func (p *SystemCPUPressureUsageEviction) pushMetric(dynamicConfig *dynamic.Configuration,
	metricName string, snapshot *MetricSnapshot) {
	//if p.metricsHistory[metricName] == nil {
	//	p.metricsHistory[metricName] = make(*MetricRing)
	//}
	//
	//if p.metricsHistory[metricName][entryName] == nil {
	//	p.metricsHistory[metricName][entryName] = make(SubEntries)
	//}

	if p.metricsHistory[metricName] == nil {
		p.metricsHistory[metricName] = CreateMetricRing(5)
	}

	p.metricsHistory[metricName].Push(snapshot)
}

func (p *SystemCPUPressureUsageEviction) ThresholdMet(_ context.Context, _ *pluginapi.Empty) (*pluginapi.ThresholdMetResponse, error) {
	p.isUnderSystemPressure = false
	resp := &pluginapi.ThresholdMetResponse{
		MetType: pluginapi.ThresholdMetType_NOT_MET,
	}

	dynamicConfig := p.conf.GetDynamicConfiguration()
	// 允许整机级别cpu load 驱逐
	if !dynamicConfig.EnableSystemUsageEviction {
		return resp, nil
	}

	p.Lock()
	defer p.Unlock()

	pressCountMetricRing := p.metricsHistory[consts.MetricCPUUsageContainer]
	if pressCountMetricRing == nil {
		general.Infof("metricName %s, pressCountMetricRing is nil", consts.MetricCPUUsageContainer)
		return resp, nil
	}
	pressCount := pressCountMetricRing.CountPress()
	general.Infof("metricName %s, pressCount: %d", consts.MetricCPUUsageContainer, pressCount)
	general.Infof("metricName %s, maxLen: %d", consts.MetricCPUUsageContainer, p.metricsHistory[consts.MetricCPUUsageContainer].MaxLen)
	pressRatio := float64(pressCount) / float64(p.metricsHistory[consts.MetricCPUUsageContainer].MaxLen)

	// 最大离线可用资源
	p.suppressionCpuQuota = p.reclaimedPoolHigherCPUBuffer
	// 目前正处于压力状态
	if p.isPress {
		if p.reclaimedPoolHigherCPUBuffer <= p.reclaimedPoolLowerCPUBuffer && pressRatio > 0.6 {
			// 五次全部压力，并且离线最大可用小于最大的离线预留
			p.isUnderSystemPressure = true
			p.suppressionCpuQuota = p.reclaimedPoolLowerCPUBuffer
			p.systemAction = actionEviction
		} else if p.reclaimedPoolHigherCPUBuffer <= p.reclaimedPoolLowerCPUBuffer {
			p.isUnderSystemPressure = true
			p.suppressionCpuQuota = p.reclaimedPoolLowerCPUBuffer
			p.systemAction = actionSuppression
		} else {
			// 当前处于压力，就要禁止调度
			p.isUnderSystemPressure = true
			p.systemAction = actionSuppression
		}
	}

	if p.isUnderSystemPressure {
		switch p.systemAction {
		case actionEviction:
			resp = &pluginapi.ThresholdMetResponse{
				MetType:       pluginapi.ThresholdMetType_HARD_MET,
				EvictionScope: EvictionScopeSystemCPU,
				Condition: &pluginapi.Condition{
					ConditionType: pluginapi.ConditionType_NODE_CONDITION,
					Effects:       []string{string(v1.TaintEffectNoSchedule)},
					ConditionName: EvictionConditionSystemCPUPressure,
					MetCondition:  true,
				},
			}
		case actionSuppression:
			resp = &pluginapi.ThresholdMetResponse{
				MetType:       pluginapi.ThresholdMetType_HARD_MET,
				EvictionScope: EvictionScopeSystemCPU,
				Condition: &pluginapi.Condition{
					ConditionType: pluginapi.ConditionType_NODE_CONDITION,
					Effects:       []string{string(v1.TaintEffectNoSchedule)},
					ConditionName: EvictionConditionSystemCPUPressure,
					MetCondition:  true,
				},
			}

		}

	}
	general.Infof("ThresholdMet result, m.isUnderSystemPressure: %+v, m.systemAction: %+v", p.isUnderSystemPressure, p.systemAction)
	p.AdjustReclaimPoolCPU()
	return resp, nil
}

func (p *SystemCPUPressureUsageEviction) AdjustReclaimPoolCPU() *error {
	reclaimCPUQuota := int64(p.suppressionCpuQuota * 100)
	general.Infof("ApplyCPUWithRelativePath: reclaimRelativeRootCgroupPath %s: reclaimCPUQuota %d", "", reclaimCPUQuota)
	err := cgroupcmutils.ApplyCPUWithRelativePath("/kubepods/besteffort", &cgroupcm.CPUData{CpuQuota: reclaimCPUQuota})
	if err != nil {
		general.Errorf("ApplyCPUWithRelativePath err: %v", err)
	}

	return nil
}

func (p *SystemCPUPressureUsageEviction) GetTopEvictionPods(_ context.Context,
	request *pluginapi.GetTopEvictionPodsRequest) (*pluginapi.GetTopEvictionPodsResponse, error) {
	// 全部驱逐

	if request == nil {
		return nil, fmt.Errorf("GetTopEvictionPods got nil request")
	} else if len(request.ActivePods) == 0 {
		general.Warningf("got empty active pods list")
		return &pluginapi.GetTopEvictionPodsResponse{}, nil
	}

	p.Lock()
	defer p.Unlock()

	dynamicConfig := p.dynamicConf.GetDynamicConfiguration()
	if !dynamicConfig.EnableSystemLevelEviction {
		return &pluginapi.GetTopEvictionPodsResponse{}, nil
	}

	general.Infof("GetTopEvictionPods condition, m.isUnderSystemPressure: %+v, "+
		"m.systemAction: %+v", p.isUnderSystemPressure, p.systemAction)

	candidatePods := []*v1.Pod{}
	// 如果有压力并且动作为驱逐 TODO：驱逐后记得改框
	if p.isUnderSystemPressure && p.systemAction == actionEviction {
		candidatePods = p.evictionHelper.filterPods(request.ActivePods, p.systemAction)
	}

	if len(candidatePods) == 0 {
		general.Warningf("got empty candidate pods list")
		return &pluginapi.GetTopEvictionPodsResponse{}, nil
	}

	general.Infof("%d candidates", len(candidatePods))

	now := time.Now()
	if !(p.lastEvictionTime.IsZero() || now.Sub(p.lastEvictionTime) >= dynamicConfig.LoadEvictionCoolDownTime) {
		general.Infof("in eviction cool-down time, skip eviction. now: %s, lastEvictionTime: %s",
			now.String(), p.lastEvictionTime.String())
		return &pluginapi.GetTopEvictionPodsResponse{}, nil
	}
	p.lastEvictionTime = now

	// 临时按照cpu申请量
	sort.Slice(candidatePods, func(i, j int) bool {
		p1 := native.GetCPUQuantity(native.SumUpPodRequestResources(candidatePods[i]))
		p2 := native.GetCPUQuantity(native.SumUpPodRequestResources(candidatePods[j]))
		com := p1.Cmp(p2)
		if com < 0 {
			return false
		} else {
			return true
		}
		//return p.getMetricHistorySumForPod(consts.MetricCPUUsageContainer, candidatePods[i]) >
		//	p.getMetricHistorySumForPod(consts.MetricCPUUsageContainer, candidatePods[j])
	})

	retLen := general.MinUInt64(request.TopN, uint64(len(candidatePods)))
	resp := &pluginapi.GetTopEvictionPodsResponse{
		TargetPods: candidatePods[:retLen],
	}

	if gracePeriod := dynamicConfig.CPUPressureEvictionConfiguration.GracePeriod; gracePeriod >= 0 {
		resp.DeletionOptions = &pluginapi.DeletionOptions{
			GracePeriodSeconds: gracePeriod,
		}
	}

	return resp, nil
}

// getMetricHistorySumForPod returns the accumulated value for the given pod
func (p *SystemCPUPressureUsageEviction) getMetricForPool(metricName string, poolName string) float64 {
	if poolName == "" {
		return 0
	}
	ret := 0.0
	if metricMap, ok := p.metricCurrent[poolName]; ok && metricMap != nil {
		return p.metricCurrent[poolName][metricName]
	}
	return ret
}
