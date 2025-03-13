module github.com/rh-ecosystem-edge/nvidia-ci

go 1.21
toolchain go1.23.7

replace (
	github.com/k8snetworkplumbingwg/sriov-network-operator => github.com/openshift/sriov-network-operator v0.0.0-20240228013256-b13485442721 // release-4.15
	github.com/openshift/api => github.com/openshift/api v0.0.0-20240228005710-4511c790cc60 // release-4.15
	github.com/openshift/assisted-service/api => github.com/openshift/assisted-service/api v0.0.0-20240222220008-d60e80f8658c // release-4.15
	github.com/openshift/assisted-service/models => github.com/openshift/assisted-service/models v0.0.0-20240222220008-d60e80f8658c // release-4.15
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.1
	github.com/openshift/installer => github.com/openshift/installer v0.9.0-master.0.20230306121016-3485fddca1c3 // master
	k8s.io/api => k8s.io/api v0.28.7
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.28.7
	k8s.io/apimachinery => k8s.io/apimachinery v0.28.7
	k8s.io/apiserver => k8s.io/apiserver v0.28.7
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.28.7
	k8s.io/client-go => k8s.io/client-go v0.28.7
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.28.7
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.28.7
	k8s.io/code-generator => k8s.io/code-generator v0.28.7
	k8s.io/component-base => k8s.io/component-base v0.28.7
	k8s.io/component-helpers => k8s.io/component-helpers v0.28.7
	k8s.io/controller-manager => k8s.io/controller-manager v0.28.7
	k8s.io/cri-api => k8s.io/cri-api v0.28.7
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.28.7
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.28.7
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.28.7
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.28.7
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.28.7
	k8s.io/kubectl => k8s.io/kubectl v0.28.7
	k8s.io/kubelet => k8s.io/kubelet v0.28.7
	k8s.io/kubernetes => k8s.io/kubernetes v1.28.7
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.28.7
	k8s.io/metrics => k8s.io/metrics v0.28.7
	k8s.io/mount-utils => k8s.io/mount-utils v0.28.7
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.28.7
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.28.7
)

require (
	github.com/Mellanox/network-operator v1.4.0
	github.com/NVIDIA/gpu-operator v1.8.3-0.20240429200431-0fe1e8db32b0
	github.com/NVIDIA/k8s-operator-libs v0.0.0-20240214071211-ea58a3ada15c
	github.com/golang/glog v1.2.4
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/onsi/ginkgo/v2 v2.17.1
	github.com/onsi/gomega v1.32.0
	github.com/openshift-kni/k8sreporter v1.0.5
	github.com/openshift/api v0.0.0-20240522145529-93d6bda14341
	github.com/openshift/client-go v0.0.1
	github.com/openshift/cluster-nfd-operator v0.0.0-20231206145954-f49a827bf617
	github.com/operator-framework/api v0.23.0
	github.com/operator-framework/operator-lifecycle-manager v0.22.0
	go.uber.org/mock v0.4.0
	gopkg.in/k8snetworkplumbingwg/multus-cni.v4 v4.0.2
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.30.1
	k8s.io/apiextensions-apiserver v0.29.3
	k8s.io/apimachinery v0.30.1
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kubectl v0.29.1
	k8s.io/utils v0.0.0-20240502163921-fe8a2dddb1d0
	sigs.k8s.io/controller-runtime v0.17.3
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/NVIDIA/k8s-kata-manager v0.0.0-20230620232711-08b57feb9b5a // indirect
	github.com/antlr/antlr4/runtime/Go/antlr/v4 v4.0.0-20230305170008-8188dc5388df // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/containernetworking/cni v1.1.2 // indirect
	github.com/containers/image/v5 v5.29.3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/docker v27.1.1+incompatible // indirect
	github.com/emicklei/go-restful/v3 v3.11.2 // indirect
	github.com/evanphx/json-patch v5.7.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.8.0 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.20.2 // indirect
	github.com/go-openapi/jsonreference v0.20.4 // indirect
	github.com/go-openapi/swag v0.22.9 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/cel-go v0.17.7 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20230502171905-255e3b9b56de // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/h2non/filetype v1.1.3 // indirect
	github.com/h2non/go-is-svg v0.0.0-20160927212452-35e8c4b0612c // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.4.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/operator-framework/operator-registry v1.35.0 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.73.1 // indirect
	github.com/regclient/regclient v0.7.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/cobra v1.8.1 // indirect
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.starlark.net v0.0.0-20231121155337-90ade8b19d09 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/exp v0.0.0-20240213143201-ec583247a57a // indirect
	golang.org/x/mod v0.17.0 // indirect
	golang.org/x/net v0.36.0 // indirect
	golang.org/x/oauth2 v0.17.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/term v0.29.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.21.1-0.20240508182429-e35e4ccd0d2d // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240213162025-012b6fc9bca9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240221002015-b0ce06bbee7c // indirect
	google.golang.org/grpc v1.61.1 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/evanphx/json-patch.v5 v5.7.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/cli-runtime v0.29.1 // indirect
	k8s.io/component-base v0.29.3 // indirect
	k8s.io/klog/v2 v2.120.1 // indirect
	k8s.io/kube-openapi v0.0.0-20240221221325-2ac9dc51f3f1 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/kustomize/api v0.16.0 // indirect
	sigs.k8s.io/kustomize/kyaml v0.16.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)
