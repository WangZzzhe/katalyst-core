package overcommit

import (
	"fmt"
	"strconv"

	"k8s.io/klog/v2"
)

func OvercommitRatioValidate(
	nodeAnnotation map[string]string,
	setOvercommitKey, predictOvercommitKey, realtimeOvercommitKey string) (float64, error) {

	// overcommit is not allowed if overcommitRatio is not set by user
	setOvercommitVal, ok := nodeAnnotation[setOvercommitKey]
	if !ok {
		return 1.0, nil
	}

	overcommitRatio, err := strconv.ParseFloat(setOvercommitVal, 64)
	if err != nil {
		return 1.0, err
	}

	predictOvercommitVal, ok := nodeAnnotation[predictOvercommitKey]
	if ok {
		predictOvercommitRatio, err := strconv.ParseFloat(predictOvercommitVal, 64)
		if err != nil {
			klog.Errorf("predict overcommit %s validate fail: %v", predictOvercommitVal, err)
		}
		if predictOvercommitRatio < overcommitRatio {
			overcommitRatio = predictOvercommitRatio
		}
	}

	realtimeOvercommitVal, ok := nodeAnnotation[realtimeOvercommitKey]
	if ok {
		realtimeOvercommitRatio, err := strconv.ParseFloat(realtimeOvercommitVal, 64)
		if err != nil {
			klog.Errorf("realtime overcommit %s validate fail: %v", realtimeOvercommitVal, err)
		}
		if realtimeOvercommitRatio < overcommitRatio {
			overcommitRatio = realtimeOvercommitRatio
		}
	}

	if overcommitRatio < 1.0 {
		err = fmt.Errorf("overcommitRatio should be greater than 1")
		klog.Error(err)
		return 1, nil
	}

	return overcommitRatio, nil
}
