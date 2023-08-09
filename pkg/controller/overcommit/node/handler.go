package node

import (
	"fmt"

	"github.com/kubewharf/katalyst-api/pkg/apis/config/v1alpha1"
	"github.com/kubewharf/katalyst-api/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/util/native"

	"k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type nodeOvercommitEvent struct {
	nodeKey   string
	configKey string
	eventType
}

type eventType string

const (
	nodeEvent   eventType = "node"
	configEvent eventType = "config"
)

func (nc *NodeOvercommitController) addNodeOvercommitConfig(obj interface{}) {
	noc, ok := obj.(*v1alpha1.NodeOvercommitConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *v1alpha1.NodeOvercommitConfig: %v", obj)
		return
	}

	klog.V(4).Infof("[noc] notice addition of NodeOvercommitConfig %s", native.GenerateUniqObjectNameKey(noc))
	nc.enqueueNodeOvercommitConfig(noc, configEvent)
}

func (nc *NodeOvercommitController) updateNodeOvercommitConfig(old, new interface{}) {
	noc, ok := new.(*v1alpha1.NodeOvercommitConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *v1alpha1.NodeOvercommitConfig: %v", noc)
		return
	}

	klog.V(4).Infof("[noc] notice update of NodeOvercommitConfig %s", native.GenerateUniqObjectNameKey(noc))
	nc.enqueueNodeOvercommitConfig(noc, configEvent)
}

func (nc *NodeOvercommitController) deleteNodeOvercommitConfig(obj interface{}) {
	noc, ok := obj.(*v1alpha1.NodeOvercommitConfig)
	if !ok {
		klog.Errorf("cannot convert obj to *v1alpha1.NodeOvercommitConfig: %v", obj)
		return
	}
	klog.V(4).Infof("[noc] notice deletion of NodeOvercommitConfig %s", native.GenerateUniqObjectNameKey(noc))

	nc.enqueueNodeOvercommitConfig(noc, configEvent)
}

func (nc *NodeOvercommitController) addNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert obj to *v1.Node: %v", obj)
		return
	}

	klog.V(4).Infof("[noc] notice addition of Node %s", node.Name)
	nc.enqueueNode(node)
}

func (nc *NodeOvercommitController) updateNode(old, new interface{}) {
	oldNode, ok := old.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert obj to *v1.Node: %v", old)
		return
	}
	newNode, ok := new.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert obj to *v1.Node: %v", new)
		return
	}
	var (
		oldLabel, newLabel string
	)
	if len(oldNode.Labels) != 0 {
		oldLabel = oldNode.Labels[consts.NodeOvercommitSelectorKey]
	}
	if len(newNode.Labels) != 0 {
		newLabel = newNode.Labels[consts.NodeOvercommitSelectorKey]
	}
	if oldLabel == newLabel {
		return
	}

	klog.V(4).Infof("[noc] notice update of Node %s", newNode.Name)
	nc.enqueueNode(newNode)
}

func (nc *NodeOvercommitController) deleteNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert obj to *v1.Node: %v", obj)
		return
	}

	klog.V(4).Infof("[noc] notice deletion of Node %s", node.Name)
	nc.enqueueNode(node)
}

func (nc *NodeOvercommitController) enqueueNodeOvercommitConfig(noc *v1alpha1.NodeOvercommitConfig, eventType eventType) {
	if noc == nil {
		klog.Warning("[noc] trying to enqueue a nil config")
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(noc)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", noc, err))
		return
	}

	nc.nocSyncQueue.Add(nodeOvercommitEvent{
		configKey: key,
		eventType: eventType,
	})
}

func (nc *NodeOvercommitController) enqueueNode(node *v1.Node) {
	if node == nil {
		klog.Warning("[noc] trying to enqueue a nil node")
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(node)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", node, err))
		return
	}

	nc.nocSyncQueue.Add(nodeOvercommitEvent{
		nodeKey:   key,
		eventType: nodeEvent,
	})
}
