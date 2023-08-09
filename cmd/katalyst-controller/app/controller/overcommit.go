package controller

import (
	"context"
	katalyst "github.com/kubewharf/katalyst-core/cmd/base"
	"github.com/kubewharf/katalyst-core/pkg/config"
	"github.com/kubewharf/katalyst-core/pkg/controller/overcommit/node"
	"k8s.io/klog/v2"
)

const (
	OvercommitControllerName = "overcommit"
)

func StartOvercommitController(
	ctx context.Context,
	controlCtx *katalyst.GenericContext,
	conf *config.Configuration,
	_ interface{},
	_ string,
) (bool, error) {
	noc, err := node.NewNodeOvercommitController(
		ctx,
		controlCtx,
		conf.GenericConfiguration,
		conf.ControllersConfiguration.OvercommitConfig,
	)
	if err != nil {
		klog.Errorf("failed to new nodeOvercommit controller")
		return false, err
	}

	go noc.Run()
	return true, nil
}
