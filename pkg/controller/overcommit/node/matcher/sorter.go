package matcher

import (
	"github.com/kubewharf/katalyst-api/pkg/apis/config/v1alpha1"
)

type NocList []*v1alpha1.NodeOvercommitConfig

type ConfigType int

const (
	ConfigDefault ConfigType = iota
	ConfigSelector
	ConfigNodeList
)

func (nl NocList) Len() int {
	return len(nl)
}

func (nl NocList) Swap(i, j int) {
	nl[i], nl[j] = nl[j], nl[i]
}

func (nl NocList) Less(i, j int) bool {
	ti := NodeOvercommitConfigType(nl[i])
	tj := NodeOvercommitConfigType(nl[j])

	if ti == tj {
		return nl[j].CreationTimestamp.Before(&nl[i].CreationTimestamp)
	}

	return ti > tj
}

func NodeOvercommitConfigType(config *v1alpha1.NodeOvercommitConfig) ConfigType {
	if config.Spec.NodeList != nil {
		return ConfigNodeList
	}

	if config.Spec.Selector != nil {
		return ConfigSelector
	}

	return ConfigDefault
}
