package loadaware

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"sync"
	"time"

	"github.com/kubewharf/katalyst-core/pkg/util/native"

	"github.com/kubewharf/katalyst-core/pkg/config/controller"

	katalyst_base "github.com/kubewharf/katalyst-core/cmd/base"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/kubewharf/katalyst-api/pkg/apis/node/v1alpha1"
	informersv1alpha1 "github.com/kubewharf/katalyst-api/pkg/client/informers/externalversions/node/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/client/control"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
)

const loadawareControllerName = "loadaware"

type Controller struct {
	sync.RWMutex

	ctx     context.Context
	workers int32

	nodeMonitorInformer informersv1alpha1.NodeMonitorInformer
	nodeLister          listerv1.NodeLister
	metricsClient       metricsclientset.Interface
	namespaceLister     listerv1.NamespaceLister
	nmUpdater           control.NmUpdater
	podLister           listerv1.PodLister

	nodePoolMap     map[int32]sets.String
	nodeStatDataMap map[string]*NodeMetricData
	podStatDataMap  map[string]*PodMetricData
	nodeToPodsMap   map[string]map[string]struct{}

	syncMetricInterval time.Duration
	listMetricTimeout  time.Duration
	syncedFunc         []cache.InformerSynced

	maxPodUsageCount          int
	enableSyncPodUsage        bool
	podUsageSelectorKey       string
	podUsageSelectorVal       string
	podUsageSelectorNamespace string

	emitter metrics.MetricEmitter
}

func NewLoadAwareController(
	ctx context.Context,
	controlCtx *katalyst_base.GenericContext,
	loadawareConf *controller.LoadAwareConfig,
) (*Controller, error) {
	ctrl := &Controller{
		ctx:                 ctx,
		workers:             int32(loadawareConf.Workers),
		nodeMonitorInformer: controlCtx.InternalInformerFactory.Node().V1alpha1().NodeMonitors(),
		nodeLister:          controlCtx.KubeInformerFactory.Core().V1().Nodes().Lister(),
		podLister:           controlCtx.KubeInformerFactory.Core().V1().Pods().Lister(),
		namespaceLister:     controlCtx.KubeInformerFactory.Core().V1().Namespaces().Lister(),
		metricsClient:       controlCtx.Client.MetricClient,
		nmUpdater:           control.NewRealNmUpdater(controlCtx.Client.InternalClient),
		nodePoolMap:         make(map[int32]sets.String),
		nodeStatDataMap:     make(map[string]*NodeMetricData),
		podStatDataMap:      make(map[string]*PodMetricData),
		nodeToPodsMap:       make(map[string]map[string]struct{}),

		emitter:            controlCtx.EmitterPool.GetDefaultMetricsEmitter(),
		syncMetricInterval: loadawareConf.SyncMetricInterval,
		listMetricTimeout:  loadawareConf.ListMetricTimeout,
		syncedFunc:         []cache.InformerSynced{},

		maxPodUsageCount:          loadawareConf.MaxPodUsageCount,
		podUsageSelectorNamespace: loadawareConf.PodUsageSelectorNamespace,
		podUsageSelectorKey:       loadawareConf.PodUsageSelectorKey,
		podUsageSelectorVal:       loadawareConf.PodUsageSelectorVal,
	}

	nodeInformer := controlCtx.KubeInformerFactory.Core().V1().Nodes().Informer()
	nodeInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    ctrl.OnNodeAdd,
			UpdateFunc: ctrl.OnNodeUpdate,
			DeleteFunc: ctrl.OnNodeDelete,
		},
	)

	podInformer := controlCtx.KubeInformerFactory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    ctrl.OnPodAdd,
			UpdateFunc: ctrl.OnPodUpdate,
			DeleteFunc: ctrl.OnPodDelete,
		},
	)

	nsInformer := controlCtx.KubeInformerFactory.Core().V1().Namespaces().Informer()
	nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{})

	ctrl.nodeMonitorInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{})

	ctrl.syncedFunc = []cache.InformerSynced{
		nodeInformer.HasSynced,
		podInformer.HasSynced,
		nsInformer.HasSynced,
		ctrl.nodeMonitorInformer.Informer().HasSynced,
	}

	return ctrl, nil
}

func (ctrl *Controller) Run() {
	defer utilruntime.HandleCrash()
	defer func() {
		klog.Infof("Shutting down %s controller", loadawareControllerName)
	}()

	if !cache.WaitForCacheSync(ctrl.ctx.Done(), ctrl.syncedFunc...) {
		utilruntime.HandleError(fmt.Errorf("unable to sync caches for %s controller", loadawareControllerName))
		return
	}

	klog.Infof("caches are synced for %s controller", loadawareControllerName)

	ctrl.Lock()
	defer ctrl.Unlock()

	if ctrl.podUsageRequired() {
		ctrl.enableSyncPodUsage = true
	}

	nodes, err := ctrl.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Fatalf("get all nodes from cache error, err:%v", err)
	}
	//init worker node pool
	for _, node := range nodes {
		bucketID := ctrl.getBucketID(node.Name)
		if pool, ok := ctrl.nodePoolMap[bucketID]; !ok {
			ctrl.nodePoolMap[bucketID] = sets.NewString(node.Name)
		} else {
			pool.Insert(node.Name)
		}
	}

	ctrl.constructNodeToPodMap()

	nodeMonitors, err := ctrl.nodeMonitorInformer.Lister().List(labels.Everything())
	if err != nil {
		klog.Errorf("get all NodeMonitor from cache error, err:%v", err)
	}

	//restore NodeMonitor from api server
	for _, nm := range nodeMonitors {
		avg15MinCacheStr := nm.Annotations[consts.Annotation15MinAvgKey]
		max1hourCacheStr := nm.Annotations[consts.Annotation1HourMaxKey]
		max1dayCacheStr := nm.Annotations[consts.Annotation1DayMaxKey]

		var avg15MinCache []corev1.ResourceList
		var max1hourCache []*ResourceListWithTime
		var max1dayCache []*ResourceListWithTime
		if err = json.Unmarshal([]byte(avg15MinCacheStr), &avg15MinCache); err != nil {
			klog.Warning("parse avg15MinCache error, err:%v", err)
		}
		if err = json.Unmarshal([]byte(max1hourCacheStr), &max1hourCache); err != nil {
			klog.Warning("parse max1hourCacheStr error, err:%v", err)
		}
		if err = json.Unmarshal([]byte(max1dayCacheStr), &max1dayCache); err != nil {
			klog.Warning("parse max1dayCacheStr error, err:%v", err)
		}
		ctrl.nodeStatDataMap[nm.Name] = &NodeMetricData{
			Latest15MinCache: avg15MinCache,
			Latest1HourCache: max1hourCache,
			Latest1DayCache:  max1dayCache,
		}
	}

	go wait.Until(ctrl.syncNode, ctrl.syncMetricInterval, ctrl.ctx.Done())

	go time.AfterFunc(TransferToCRStoreTime, func() {
		klog.Infof("start transferMetaToCRStore")
		wait.Until(func() {
			ctrl.transferMetaToCRStore()
		}, TransferToCRStoreTime, ctrl.ctx.Done())
	})

	go wait.Until(func() {
		ctrl.reportNodeLoadMetric()
	}, 1*time.Minute, ctrl.ctx.Done())

	go wait.Until(func() {
		ctrl.podWorker()
	}, ctrl.syncMetricInterval, ctrl.ctx.Done())

	go wait.Until(func() {
		ctrl.reCleanPodData()
	}, 5*time.Minute, ctrl.ctx.Done())

	go wait.Until(func() {
		ctrl.checkPodUsageRequired()
	}, 1*time.Minute, ctrl.ctx.Done())
}

func (ctrl *Controller) syncNode() {
	// list node metrics
	nodeMetricsMap, err := ctrl.listNodeMetrics()
	if err != nil {
		klog.Errorf("list node metrics fail: %v", err)
		return
	}

	wg := sync.WaitGroup{}
	for i := int32(0); i < ctrl.workers; i++ {
		wg.Add(1)
		go func(id int32) {
			ctrl.worker(id, nodeMetricsMap)
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func (ctrl *Controller) worker(i int32, nodeMetricsMap map[string]*v1beta1.NodeMetrics) {
	ctrl.RLock()
	nodeNames, ok := ctrl.nodePoolMap[i]
	ctrl.RUnlock()

	if !ok {
		return
	}
	for name := range nodeNames {
		nodeMetrics, ok := nodeMetricsMap[name]
		if !ok {
			klog.Errorf("%s node metrics miss", name)
			continue
		}
		now := metav1.Now()
		if isNodeMetricsExpired(nodeMetrics, now) {
			klog.Errorf("node %s node metrics expired, metricsTime: %v", name, nodeMetrics.Timestamp.String())
			continue
		}

		ctrl.Lock()
		nodeMetricData, exist := ctrl.nodeStatDataMap[name]
		if !exist {
			nodeMetricData = &NodeMetricData{}
			ctrl.nodeStatDataMap[name] = nodeMetricData
		}
		// build NodeMonitor podUsage
		podUsage := make(map[string]corev1.ResourceList)
		if pods, ok := ctrl.nodeToPodsMap[name]; ok {
			for podName := range pods {
				if podMetaData, exist := ctrl.podStatDataMap[podName]; exist {
					podUsage[podName] = podMetaData.Avg5Min.DeepCopy()
				}
			}
		}
		ctrl.Unlock()
		refreshNodeMetricData(nodeMetricData, nodeMetrics, now.Time)
		err := ctrl.createOrUpdateNodeMonitorStatus(nodeMetricData, name, now, podUsage)
		if err != nil {
			klog.Errorf("createOrUpdateNodeMonitorStatus fail, node: %v, err: %v", name, err)
			continue
		}
	}
}

func (ctrl *Controller) podWorker() {
	if !ctrl.enableSyncPodUsage {
		return
	}
	nsList, err := ctrl.namespaceLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("get all namespaces failed, err:%v", err)
		return
	}
	for _, ns := range nsList {
		podMetricsList, err := ctrl.getPodMetrics(ns.Name)
		if err != nil {
			klog.Errorf("get podMetrics of namespace:%s failed, err:%v", ns.Name, err)
			continue
		}
		ctrl.Lock()
		for _, podMetrics := range podMetricsList.Items {
			now := metav1.Now()
			if isPodMetricsExpired(&podMetrics, now) {
				klog.Errorf("podMetrics is expired, podName: %v", podMetrics.Name)
				continue
			}
			namespacedName := native.GenerateNamespaceNameKey(podMetrics.Namespace, podMetrics.Name)
			metricData, exist := ctrl.podStatDataMap[namespacedName]
			if !exist {
				metricData = &PodMetricData{}
				ctrl.podStatDataMap[namespacedName] = metricData
			}

			refreshPodMetricData(metricData, &podMetrics)
		}
		ctrl.Unlock()
	}
}

func (ctrl *Controller) listNodeMetrics() (map[string]*v1beta1.NodeMetrics, error) {
	timeout, cancel := context.WithTimeout(ctrl.ctx, ctrl.listMetricTimeout)
	defer cancel()

	nodeMetricsList, err := ctrl.metricsClient.MetricsV1beta1().NodeMetricses().List(timeout, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	res := make(map[string]*v1beta1.NodeMetrics)
	for _, nm := range nodeMetricsList.Items {
		res[nm.Name] = nm.DeepCopy()
	}

	return res, nil
}

func (ctrl *Controller) getPodMetrics(namespace string) (*v1beta1.PodMetricsList, error) {
	timeout, cancel := context.WithTimeout(ctrl.ctx, ctrl.listMetricTimeout)
	defer cancel()
	mc := ctrl.metricsClient.MetricsV1beta1()
	return mc.PodMetricses(namespace).List(timeout, metav1.ListOptions{})
}

func (ctrl *Controller) createOrUpdateNodeMonitorStatus(metricData *NodeMetricData, nodeName string, now metav1.Time, podUsages map[string]corev1.ResourceList) error {
	nm, err := ctrl.nodeMonitorInformer.Lister().Get(nodeName)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		metricData.lock.RLock()
		latest15MinCache, _ := json.Marshal(metricData.Latest15MinCache)
		latest1HourCache, _ := json.Marshal(metricData.Latest1HourCache)
		latest1DayCache, _ := json.Marshal(metricData.Latest1DayCache)
		metricData.lock.RUnlock()
		nodeMonitor := &v1alpha1.NodeMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Annotations: map[string]string{
					consts.Annotation15MinAvgKey: string(latest15MinCache),
					consts.Annotation1HourMaxKey: string(latest1HourCache),
					consts.Annotation1DayMaxKey:  string(latest1DayCache),
				},
			},
			Spec: v1alpha1.NodeMonitorSpec{
				ReportInterval: &metav1.Duration{
					Duration: ctrl.syncMetricInterval,
				},
			},
		}

		nm, err = ctrl.nmUpdater.CreateNm(ctrl.ctx, nodeMonitor, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		klog.V(5).Infof("create nodeMonitor success, nodeName: %v", nodeName)
	}
	nm.Status.UpdateTime = &now
	metricData.lock.RLock()
	nm.Status.NodeUsage = map[string]corev1.ResourceList{
		consts.Usage5MinAvgKey:  metricData.Avg5Min.DeepCopy(),
		consts.Usage15MinAvgKey: metricData.Avg15Min.DeepCopy(),
		consts.Usage1HourMaxKey: metricData.Max1Hour.DeepCopy(),
		consts.Usage1DayMaxKey:  metricData.Max1Day.DeepCopy(),
	}
	if ctrl.enableSyncPodUsage {
		if len(podUsages) > 0 {
			nm.Status.PodUsage = getTopNPodUsages(podUsages, ctrl.maxPodUsageCount)
		}
	} else {
		nm.Status.PodUsage = nil
	}

	metricData.lock.RUnlock()

	_, err = ctrl.nmUpdater.UpdateNmStatus(ctrl.ctx, nm, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.V(5).Infof("update nodeMonitorStatus success, nodeName: %v", nodeName)
	return nil
}

func (ctrl *Controller) getBucketID(name string) int32 {
	hash := int64(crc32.ChecksumIEEE([]byte(name)))
	size := hash % int64(ctrl.workers)
	return int32(size)
}

func (ctrl *Controller) transferMetaToCRStore() {
	copyNodeStatDataMap := ctrl.getNodeStatDataMap()
	for nodeName, metricData := range copyNodeStatDataMap {
		oldNm, _ := ctrl.nodeMonitorInformer.Lister().Get(nodeName)
		if oldNm == nil {
			continue
		}
		newNm := oldNm.DeepCopy()
		metricData.lock.RLock()
		latest15MinCache, _ := json.Marshal(metricData.Latest15MinCache)
		latest1HourCache, _ := json.Marshal(metricData.Latest1HourCache)
		latest1DayCache, _ := json.Marshal(metricData.Latest1DayCache)
		metricData.lock.RUnlock()
		newNm.Annotations[consts.Annotation15MinAvgKey] = string(latest15MinCache)
		newNm.Annotations[consts.Annotation1HourMaxKey] = string(latest1HourCache)
		newNm.Annotations[consts.Annotation1DayMaxKey] = string(latest1DayCache)

		_, err := ctrl.nmUpdater.PatchNm(ctrl.ctx, newNm.Name, oldNm, newNm)
		if err != nil {
			klog.Errorf("patch NodeMonitor error, nodeName %v, err: %v", nodeName, err)
			continue
		}
		klog.V(5).Infof("patch NodeMonitor Annotations success, nodeName: %v", nodeName)
	}
}

func (ctrl *Controller) getNodeStatDataMap() map[string]*NodeMetricData {
	ctrl.RLock()
	defer ctrl.RUnlock()
	meta := make(map[string]*NodeMetricData)
	for nodeName, value := range ctrl.nodeStatDataMap {
		meta[nodeName] = value
	}
	return meta
}

func (ctrl *Controller) reportNodeLoadMetric() {
	ctrl.RLock()
	defer ctrl.RUnlock()
	resourceDims := []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory}
	for _, resourceName := range resourceDims {
		resultMap := make(map[int64]*int64)
		for _, data := range ctrl.nodeStatDataMap {
			data.lock.RLock()
			load := calNodeLoad(resourceName, data.LatestUsage, data.TotalRes)
			data.lock.RUnlock()
			idx := load / 10
			if count, ok := resultMap[idx]; !ok {
				i := int64(1)
				resultMap[idx] = &i
			} else {
				*count++
			}
		}
		for idx, level := range levels {
			typeTag := metrics.MetricTag{Key: metricTagType, Val: string(resourceName)}
			levelTag := metrics.MetricTag{Key: metricTagLevel, Val: level}
			if count, ok := resultMap[int64(idx)]; ok {
				_ = ctrl.emitter.StoreFloat64(loadAwareMetricName, float64(*count), metrics.MetricTypeNameRaw, typeTag, levelTag)
			} else {
				_ = ctrl.emitter.StoreFloat64(loadAwareMetricName, 0, metrics.MetricTypeNameRaw, typeTag, levelTag)
			}
		}
	}
}

func (ctrl *Controller) podUsageRequired() bool {
	pods, err := ctrl.podLister.Pods(ctrl.podUsageSelectorNamespace).
		List(labels.SelectorFromSet(map[string]string{ctrl.podUsageSelectorKey: ctrl.podUsageSelectorVal}))
	if err != nil {
		klog.Errorf("get pod usage pods err: %v", err)
		return false
	}
	return len(pods) > 0
}

func (ctrl *Controller) constructNodeToPodMap() {
	pods, err := ctrl.podLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list all pod error, err:%v", err)
		return
	}
	for _, pod := range pods {
		if len(pod.Spec.NodeName) > 0 {
			if podMap, ok := ctrl.nodeToPodsMap[pod.Spec.NodeName]; ok {
				podMap[native.GenerateNamespaceNameKey(pod.Namespace, pod.Name)] = struct{}{}
			} else {
				ctrl.nodeToPodsMap[pod.Spec.NodeName] = map[string]struct{}{
					native.GenerateNamespaceNameKey(pod.Namespace, pod.Name): {},
				}
			}
		}
	}
}

// reCleanPodData Because the data in the podStatDataMap is pulled from the metrics-server with a specific interval,
// relying solely on pod delete events to remove data from the podStatDataMap can result in data residue.
// Here we need to actively perform cleanup data residue.
func (ctrl *Controller) reCleanPodData() {
	ctrl.Lock()
	defer ctrl.Unlock()
	pods, err := ctrl.podLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("get all pods error, err:=%v", err)
		return
	}
	existPod := make(map[string]struct{})
	for _, pod := range pods {
		existPod[native.GenerateNamespaceNameKey(pod.Namespace, pod.Name)] = struct{}{}
	}
	for podName := range ctrl.podStatDataMap {
		if _, ok := existPod[podName]; !ok {
			delete(ctrl.podStatDataMap, podName)
		}
	}
}

func (ctrl *Controller) checkPodUsageRequired() {
	if ctrl.podUsageRequired() {
		podUsageUnrequiredCount = 0
		ctrl.enableSyncPodUsage = true
	} else {
		podUsageUnrequiredCount++
		if podUsageUnrequiredCount >= 5 {
			ctrl.enableSyncPodUsage = false
			podUsageUnrequiredCount = 0
		}
	}
}

func (md *NodeMetricData) ifCanInsertLatest1HourCache(now time.Time) bool {
	if len(md.Latest1HourCache) == 0 {
		return true
	}
	latestData := md.Latest1HourCache[len(md.Latest1HourCache)-1]
	lastTime := time.Unix(latestData.Ts, 0)
	if now.After(lastTime.Add(15*time.Minute)) || now.Equal(lastTime.Add(15*time.Minute)) {
		return true
	}
	return false
}

func (md *NodeMetricData) ifCanInsertLatest1DayCache(now time.Time) bool {
	if len(md.Latest1DayCache) == 0 {
		return true
	}
	latestData := md.Latest1DayCache[len(md.Latest1DayCache)-1]
	lastTime := time.Unix(latestData.Ts, 0)
	if now.After(lastTime.Add(1*time.Hour)) || now.Equal(lastTime.Add(1*time.Hour)) {
		return true
	}
	return false
}
