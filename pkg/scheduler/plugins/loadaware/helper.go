package loadaware

import (
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/kubewharf/katalyst-api/pkg/apis/resourceportrait/v1alpha1"
	"github.com/kubewharf/katalyst-core/pkg/util/native"
)

type Items []v1alpha1.Item

func (it Items) Len() int {
	return len(it)
}

func (it Items) Swap(i, j int) {
	it[i], it[j] = it[j], it[i]
}

func (it Items) Less(i, j int) bool {
	if it[i].Timestamp == 0 {
		return true
	}
	if it[j].Timestamp == 0 {
		return false
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.Local
	}
	// sort sample timestamp hour
	houri := time.Unix(it[i].Timestamp, 0).In(location).Hour()
	hourj := time.Unix(it[j].Timestamp, 0).In(location).Hour()

	return houri < hourj
}

func podToWorkloadByOwner(pod *v1.Pod) (string, string, bool) {
	for _, owner := range pod.OwnerReferences {
		kind := owner.Kind
		switch kind {
		// resource portrait time series predicted and stored by deployment, but pod owned by rs
		case "ReplicaSet":
			names := strings.Split(owner.Name, "-")
			if len(names) <= 1 {
				klog.Warningf("unexpected rs name: %v", owner.Name)
				return "", "", false
			}
			names = names[0 : len(names)-1]
			return strings.Join(names, "-"), "Deployment", true
		default:
			return owner.Name, kind, true
		}
	}

	return "", "", false
}

func resourcePortraitResultName(workloadName, workloadType string, portraitNameFmt, portraitAppName string) string {
	return strings.ToLower(fmt.Sprintf(portraitNameFmt, portraitAppName, workloadType, workloadName))
}

func cpuTimeSeriesByRequest(podResource v1.ResourceList, scaleFactor float64) []float64 {
	timeSeries := make([]float64, portraitItemsLength, portraitItemsLength)

	if podResource.Cpu() != nil && !podResource.Cpu().IsZero() {
		cpuUsage := native.MultiplyResourceQuantity(v1.ResourceCPU, *podResource.Cpu(), scaleFactor)
		for i := range timeSeries {
			timeSeries[i] = float64(cpuUsage.MilliValue())
		}
	}
	return timeSeries
}

func memoryTimeSeriesByRequest(podResource v1.ResourceList, scaleFactor float64) []float64 {
	timeSeries := make([]float64, portraitItemsLength, portraitItemsLength)

	if podResource.Memory() != nil && !podResource.Memory().IsZero() {
		memoryUsage := native.MultiplyResourceQuantity(v1.ResourceMemory, *podResource.Memory(), scaleFactor)
		for i := range timeSeries {
			timeSeries[i] = float64(memoryUsage.Value())
		}
	}
	return timeSeries
}
