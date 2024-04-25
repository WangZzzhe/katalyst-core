package controller

import (
	"context"

	"k8s.io/klog/v2"

	katalyst "github.com/kubewharf/katalyst-core/cmd/base"
	"github.com/kubewharf/katalyst-core/pkg/config"
	"github.com/kubewharf/katalyst-core/pkg/controller/loadaware"
)

const (
	LoadAwareControllerName = "loadaware"
)

func StartLoadAwareController(
	ctx context.Context,
	controlCtx *katalyst.GenericContext,
	conf *config.Configuration,
	_ interface{},
	_ string,
) (bool, error) {
	la, err := loadaware.NewLoadAwareController(
		ctx,
		controlCtx,
		conf.ControllersConfiguration.LoadAwareConfig,
	)
	if err != nil {
		klog.Errorf("failed to new loadAware controller")
		return false, err
	}

	go la.Run()
	return true, nil
}
