package controller

import "time"

type LoadAwareConfig struct {
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

func NewLoadAwareConfig() *LoadAwareConfig {
	return &LoadAwareConfig{}
}
