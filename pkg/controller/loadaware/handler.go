package loadaware

import (
	"github.com/kubewharf/katalyst-core/pkg/util/native"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (ctrl *Controller) OnNodeAdd(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Error("transfer to v1.Node error")
	}
	klog.V(5).Infof("OnNodeAdd node %v add event", node.Name)
	ctrl.Lock()
	defer ctrl.Unlock()
	bucketID := ctrl.getBucketID(node.Name)
	if pool, ok := ctrl.nodePoolMap[bucketID]; ok {
		pool.Insert(node.Name)
	} else {
		pool = sets.NewString(node.Name)
		ctrl.nodePoolMap[bucketID] = pool
	}
	metricData, exist := ctrl.nodeStatDataMap[node.Name]
	if !exist {
		metricData = &NodeMetricData{}
		ctrl.nodeStatDataMap[node.Name] = metricData
	}
	metricData.TotalRes = node.Status.Allocatable.DeepCopy()
}

func (ctrl *Controller) OnNodeUpdate(oldObj, obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Error("transfer to v1.Node error")
	}
	klog.V(5).Infof("OnNodeUpdate node %v update event", node.Name)
	ctrl.Lock()
	defer ctrl.Unlock()
	metricData, exist := ctrl.nodeStatDataMap[node.Name]
	if !exist {
		metricData = &NodeMetricData{}
		ctrl.nodeStatDataMap[node.Name] = metricData
	}
	metricData.TotalRes = node.Status.Allocatable.DeepCopy()
}

func (ctrl *Controller) OnNodeDelete(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Error("transfer to v1.Node error")
	}
	klog.V(5).Infof("OnNodeDelete node %v delete event", node.Name)
	ctrl.Lock()
	bucketID := ctrl.getBucketID(node.Name)
	if pool, ok := ctrl.nodePoolMap[bucketID]; ok {
		pool.Delete(node.Name)
	}
	delete(ctrl.nodeStatDataMap, node.Name)
	ctrl.Unlock()
	err := ctrl.nmUpdater.DeleteNm(ctrl.ctx, node.Name, v12.DeleteOptions{})
	if err != nil {
		klog.Errorf("delete nm %s fail: %v", node.Name, err)
	}
}

func (ctrl *Controller) OnPodAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Error("transfer to v1.Pod error")
	}
	klog.V(5).Infof("OnPodAdd pod %v add event", pod.Name)
	ctrl.Lock()
	defer ctrl.Unlock()
	if ctrl.podUsageSelectorKey != "" {
		if value, exist := pod.Labels[ctrl.podUsageSelectorKey]; exist && value == ctrl.podUsageSelectorVal {
			klog.Info("start sync pod usage to nodeMonitor")
			ctrl.enableSyncPodUsage = true
		}
	}

	if len(pod.Spec.NodeName) == 0 {
		return
	}
	podName := native.GenerateNamespaceNameKey(pod.Namespace, pod.Name)
	if existPods, ok := ctrl.nodeToPodsMap[pod.Spec.NodeName]; ok {
		existPods[podName] = struct{}{}
	} else {
		existPods = make(map[string]struct{})
		existPods[podName] = struct{}{}
		ctrl.nodeToPodsMap[pod.Spec.NodeName] = existPods
	}
}

func (ctrl *Controller) OnPodUpdate(oldObj, newObj interface{}) {
	var pod *v1.Pod
	switch t := newObj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			klog.Errorf("cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("cannot convert to *v1.Pod: %v", t)
		return
	}

	klog.V(5).Infof("OnPodUpdate node %v update event", pod.Name)
	if len(pod.Spec.NodeName) == 0 {
		return
	}
	ctrl.Lock()
	defer ctrl.Unlock()
	podName := native.GenerateNamespaceNameKey(pod.Namespace, pod.Name)
	if existPods, ok := ctrl.nodeToPodsMap[pod.Spec.NodeName]; ok {
		existPods[podName] = struct{}{}
	} else {
		existPods = make(map[string]struct{})
		existPods[podName] = struct{}{}
		ctrl.nodeToPodsMap[pod.Spec.NodeName] = existPods
	}
}

func (ctrl *Controller) OnPodDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Error("transfer to v1.Pod error")
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Error("couldn't get object from tombstone %#v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			klog.Error("tombstone contained object that is not a pod %#v", obj)
			return
		}
	}
	klog.V(5).Infof("OnPodDelete node %v delete event", pod.Name)
	ctrl.Lock()
	defer ctrl.Unlock()
	if ctrl.podUsageSelectorVal != "" {
		if value, exist := pod.Labels[ctrl.podUsageSelectorKey]; exist && value == ctrl.podUsageSelectorVal {
			klog.Info("stop sync pod usage to nodeMonitor")
			ctrl.enableSyncPodUsage = false
		}
	}
	podName := native.GenerateNamespaceNameKey(pod.Namespace, pod.Name)
	delete(ctrl.podStatDataMap, podName)
	if len(pod.Spec.NodeName) == 0 {
		return
	}
	if existPods, ok := ctrl.nodeToPodsMap[pod.Spec.NodeName]; ok {
		delete(existPods, podName)
	}
}
