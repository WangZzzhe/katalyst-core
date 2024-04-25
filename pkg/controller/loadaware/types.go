package loadaware

import (
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
)

const (
	Avg5MinPointNumber  = 5
	Avg15MinPointNumber = 15
	Max1HourPointNumber = 4
	Max1DayPointNumber  = 24

	DefaultSyncWorkers    = 3
	DefaultSyncInterval   = 1 * time.Minute
	NodeMetricExpiredTime = 3 * time.Minute
	TransferToCRStoreTime = 5 * time.Minute
)

const (
	loadAwareMetricName = "node_load"
	metricTagType       = "type"
	metricTagLevel      = "level"
)

var (
	levels = []string{"0-10", "10-20", "20-30", "30-40", "40-50", "50-60", "60-70", "70-80", "80-90", "90-100"}

	podUsageUnrequiredCount = 0
)

type NodeMetricData struct {
	lock             sync.RWMutex
	LatestUsage      v1.ResourceList
	TotalRes         v1.ResourceList
	Avg5Min          v1.ResourceList
	Avg15Min         v1.ResourceList
	Max1Hour         v1.ResourceList
	Max1Day          v1.ResourceList
	Latest15MinCache []v1.ResourceList       //latest 15 1min_avg_data
	Latest1HourCache []*ResourceListWithTime //latest 4 15min_max_data
	Latest1DayCache  []*ResourceListWithTime //latest 24 1hour_max_data
}

type PodMetricData struct {
	lock            sync.RWMutex
	LatestUsage     v1.ResourceList
	Avg5Min         v1.ResourceList
	Latest5MinCache []v1.ResourceList //latest 15 1min_avg_data
}

// ResourceListWithTime ...
type ResourceListWithTime struct {
	v1.ResourceList `json:"R,omitempty"`
	Ts              int64 `json:"T,omitempty"`
}
