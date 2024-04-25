package options

import (
	"time"

	cliflag "k8s.io/component-base/cli/flag"

	"github.com/kubewharf/katalyst-core/pkg/config/controller"
)

type LoadAwareOptions struct {
	// number of workers to sync node metrics
	Workers int
	// time interval of sync node metrics
	SyncMetricInterval time.Duration
	// timeout of list metrics from apiserver
	ListMetricTimeout time.Duration

	// pod selector for checking if pod usage is required
	PodUsageSelectorNamespace string
	PodUsageSelectorKey       string
	PodUsageSelectorVal       string

	MaxPodUsageCount int
}

func NewLoadAwareOptions() *LoadAwareOptions {
	return &LoadAwareOptions{
		Workers:                   3,
		SyncMetricInterval:        time.Minute * 1,
		ListMetricTimeout:         time.Second * 10,
		PodUsageSelectorNamespace: "",
		PodUsageSelectorKey:       "",
		PodUsageSelectorVal:       "",
		MaxPodUsageCount:          20,
	}
}

func (o *LoadAwareOptions) AddFlags(fss *cliflag.NamedFlagSets) {
	fs := fss.FlagSet("loadAware")

	fs.IntVar(&o.Workers, "loadaware-sync-workers", o.Workers,
		"num of workers to sync node metrics")
	fs.DurationVar(&o.SyncMetricInterval, "loadaware-sync-interval", o.SyncMetricInterval,
		"interval of syncing node metrics")
	fs.DurationVar(&o.ListMetricTimeout, "loadaware-list-metric-timeout", o.ListMetricTimeout,
		"timeout duration when list metrics from metrics server")

	fs.StringVar(&o.PodUsageSelectorNamespace, "loadaware-podusage-selector-namespace", o.PodUsageSelectorNamespace,
		"pod namespace used to detect whether podusage should be calculated")
	fs.StringVar(&o.PodUsageSelectorKey, "loadaware-podusage-selector-key", o.PodUsageSelectorKey,
		"pod label key used to detect whether podusage should be calculated")
	fs.StringVar(&o.PodUsageSelectorVal, "loadaware-podusage-selector-val", o.PodUsageSelectorVal,
		"pod label value used to detect whether podusage should be calculated")
	fs.IntVar(&o.MaxPodUsageCount, "loadaware-max-podusage-count", o.MaxPodUsageCount,
		"max podusage count on nodemonitor")
}

func (o *LoadAwareOptions) ApplyTo(c *controller.LoadAwareConfig) error {
	c.Workers = o.Workers
	c.SyncMetricInterval = o.SyncMetricInterval
	c.ListMetricTimeout = o.ListMetricTimeout
	c.PodUsageSelectorNamespace = o.PodUsageSelectorNamespace
	c.PodUsageSelectorKey = o.PodUsageSelectorKey
	c.PodUsageSelectorVal = o.PodUsageSelectorVal
	c.MaxPodUsageCount = o.MaxPodUsageCount
	return nil
}

func (o *LoadAwareOptions) Config() (*controller.LoadAwareConfig, error) {
	c := &controller.LoadAwareConfig{}
	if err := o.ApplyTo(c); err != nil {
		return nil, err
	}

	return c, nil
}
