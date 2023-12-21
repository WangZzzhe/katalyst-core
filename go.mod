module github.com/kubewharf/katalyst-core

go 1.18

require (
	github.com/alecthomas/units v0.0.0-20190924025748-f65c72e2690d
	github.com/containerd/cgroups v1.0.1
	github.com/fsnotify/fsnotify v1.5.1
	github.com/google/cadvisor v0.44.1
	github.com/kubewharf/katalyst-api v0.3.4-0.20231204022248-bfefbbd96f52
	github.com/opencontainers/runc v1.1.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.1
	github.com/spf13/cobra v1.4.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.1
	go.opentelemetry.io/otel v0.20.0
	go.opentelemetry.io/otel/exporters/metric/prometheus v0.20.0
	go.opentelemetry.io/otel/metric v0.20.0
	go.opentelemetry.io/otel/sdk v0.20.0
	go.opentelemetry.io/otel/sdk/export/metric v0.20.0
	go.opentelemetry.io/otel/sdk/metric v0.20.0
	go.uber.org/atomic v1.7.0
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8
	google.golang.org/grpc v1.51.0
	k8s.io/api v0.24.6
	k8s.io/apimachinery v0.24.6
	k8s.io/apiserver v0.17.17
	k8s.io/client-go v0.23.5
	k8s.io/component-base v0.24.6
	k8s.io/klog/v2 v2.80.1
	k8s.io/kube-aggregator v0.17.17
	k8s.io/kubernetes v1.24.6
	k8s.io/metrics v0.17.17
	k8s.io/utils v0.0.0-20221108210102-8e77b1f39fe2
	sigs.k8s.io/controller-runtime v0.11.2
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cilium/ebpf v0.7.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20190321100706-95778dfbb74e // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/cyphar/filepath-securejoin v0.2.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/docker/docker v20.10.12+incompatible // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/emicklei/go-restful v2.16.0+incompatible // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.19.6 // indirect
	github.com/go-openapi/spec v0.19.3 // indirect
	github.com/go-openapi/swag v0.19.15 // indirect
	github.com/godbus/dbus/v5 v5.0.6 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/hashicorp/golang-lru v0.5.1 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/moby/sys/mountinfo v0.6.0 // indirect
	github.com/moby/term v0.0.0-20210619224110-3f7ff695adc6 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20210326190908-1c3f411f0417 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/spf13/afero v1.2.2 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	go.etcd.io/etcd v0.0.0-20191023171146-3cf2f69b5738 // indirect
	go.opentelemetry.io/otel/trace v0.20.0 // indirect
	go.uber.org/goleak v1.2.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.19.1 // indirect
	golang.org/x/crypto v0.0.0-20220315160706-3147a52a75dd // indirect
	golang.org/x/net v0.9.0 // indirect
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8 // indirect
	golang.org/x/sys v0.7.0 // indirect
	golang.org/x/term v0.7.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20220502173005-c8bf987b8c21 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.23.5 // indirect
	k8s.io/cloud-provider v0.17.17 // indirect
	k8s.io/csi-translation-lib v0.17.17 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20220803162953-67bda5d908f1 // indirect
	k8s.io/kube-scheduler v0.17.17 // indirect
	sigs.k8s.io/structured-merge-diff/v2 v2.0.1 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace (
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.0.0-20170729233727-0c5108395e2d
	github.com/kubewharf/katalyst-api => github.com/WangZzzhe/katalyst-api v0.0.0-20231220062146-eaf3aab44dd0
	google.golang.org/grpc => google.golang.org/grpc v1.23.1
	k8s.io/api => k8s.io/api v0.17.17
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.17.17
	k8s.io/apimachinery => k8s.io/apimachinery v0.17.17
	k8s.io/apiserver => k8s.io/apiserver v0.17.17
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.17.17
	k8s.io/client-go => k8s.io/client-go v0.17.17
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.17.17
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.17.17
	k8s.io/code-generator => k8s.io/code-generator v0.17.17
	k8s.io/component-base => k8s.io/component-base v0.17.17
	k8s.io/component-helpers => k8s.io/component-helpers v0.17.17
	k8s.io/controller-manager => k8s.io/controller-manager v0.17.17
	k8s.io/cri-api => k8s.io/cri-api v0.17.17
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.17.17
	k8s.io/klog => k8s.io/klog v1.0.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.17.17
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.17.17
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.17.17
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.17.17
	k8s.io/kubectl => k8s.io/kubectl v0.17.17
	k8s.io/kubelet => github.com/kubewharf/kubelet v1.24.6-kubewharf.7
	k8s.io/kubernetes => k8s.io/kubernetes v1.17.7
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.17.17
	k8s.io/metrics => k8s.io/metrics v0.17.17
	k8s.io/mount-utils => k8s.io/mount-utils v0.17.17
	k8s.io/node-api => k8s.io/node-api v0.17.17
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.22.8
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.17.17
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.17.17
	k8s.io/sample-controller => k8s.io/sample-controller v0.17.17
	sigs.k8s.io/json => sigs.k8s.io/json v0.0.0-20211020170558-c049b76a60c6
)
