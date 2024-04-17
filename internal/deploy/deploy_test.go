package deploy

import (
	"errors"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/eco-goinfra/pkg/clients"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("GetBundleConfig", func() {

	const logLevel = 100
	var deploy Deploy

	BeforeEach(func() {

		deploy = NewDeploy(nil)
	})

	It("should get the default value if not set", func() {

		bundleConfig, err := deploy.GetBundleConfig(logLevel)
		Expect(err).NotTo(HaveOccurred())
		Expect(bundleConfig.BundleImage).To(Equal("registry.gitlab.com/nvidia/kubernetes/gpu-operator/staging/gpu-operator-bundle:master-latest"))
	})

	It("should override the default value", func() {

		const bundleImage = "registry/org/image:tag"

		os.Setenv("GPU_BUNDLE_IMAGE", bundleImage)

		bundleConfig, err := deploy.GetBundleConfig(logLevel)
		Expect(err).NotTo(HaveOccurred())
		Expect(bundleConfig.BundleImage).To(Equal(bundleImage))
	})
})

var _ = Describe("CreateAndLabelNamespaceIfNeeded", func() {

	const (
		logLevel = 100
		ns       = "mynamespace"
	)
	var (
		ctrl          *gomock.Controller
		mockCoreV1    *MockCoreV1Interface
		mockNamespace *MockNamespaceInterface
		deploy        Deploy
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		mockCoreV1 = NewMockCoreV1Interface(ctrl)
		mockNamespace = NewMockNamespaceInterface(ctrl)

		deploy = NewDeploy(&clients.Settings{
			CoreV1Interface: mockCoreV1,
		})
	})

	It("should do nothing if the namespace already exists", func() {

		returnedNs := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}

		mockCoreV1.EXPECT().Namespaces().Return(mockNamespace).AnyTimes()

		mockNamespace.EXPECT().Get(gomock.Any(), ns, gomock.Any()).Return(returnedNs, nil)

		_, err := deploy.CreateAndLabelNamespaceIfNeeded(logLevel, ns, map[string]string{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail if it fails to create the namespace", func() {

		mockCoreV1.EXPECT().Namespaces().Return(mockNamespace).AnyTimes()

		notFoundErr := k8serrors.NewNotFound(k8sschema.GroupResource{}, "")
		mockNamespace.EXPECT().Get(gomock.Any(), ns, gomock.Any()).Return(nil, notFoundErr).Times(2)
		mockNamespace.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

		_, err := deploy.CreateAndLabelNamespaceIfNeeded(logLevel, ns, map[string]string{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create namespace"))
	})

	It("should fail if it fails to label the namespace", func() {

		mockCoreV1.EXPECT().Namespaces().Return(mockNamespace).AnyTimes()

		notFoundErr := k8serrors.NewNotFound(k8sschema.GroupResource{}, "")
		mockNamespace.EXPECT().Get(gomock.Any(), ns, gomock.Any()).Return(nil, notFoundErr).Times(2)
		mockNamespace.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(&v1.Namespace{}, nil)
		mockNamespace.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

		_, err := deploy.CreateAndLabelNamespaceIfNeeded(logLevel, ns, map[string]string{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to label namespace mynamespace"))
	})

	It("should create the namespace if it doesn't exists", func() {

		mockCoreV1.EXPECT().Namespaces().Return(mockNamespace).AnyTimes()

		notFoundErr := k8serrors.NewNotFound(k8sschema.GroupResource{}, "")
		mockNamespace.EXPECT().Get(gomock.Any(), ns, gomock.Any()).Return(nil, notFoundErr).Times(2)
		mockNamespace.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(&v1.Namespace{}, nil)
		mockNamespace.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).Return(&v1.Namespace{}, nil)

		_, err := deploy.CreateAndLabelNamespaceIfNeeded(logLevel, ns, map[string]string{})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DeployBundle", func() {

	const (
		logLevel = 100
		ns       = "mynamespace"
	)

	var (
		deploy Deploy
	)

	BeforeEach(func() {

		deploy = NewDeploy(nil)
	})

	It("should fail if operator-sdk fails", func() {

		bundleConfig := &BundleConfig{
			BundleImage: "nosuchimage",
		}
		err := deploy.DeployBundle(logLevel, bundleConfig, ns, 5*time.Minute)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("WaitForReadyStatus", func() {

	const (
		logLevel = 100
		name     = "mydeployment"
		ns       = "mynamespace"
	)
	var (
		ctrl           *gomock.Controller
		mockAppsV1     *MockAppsV1Interface
		mockDeployment *MockDeploymentInterface
		deploy         Deploy
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		mockAppsV1 = NewMockAppsV1Interface(ctrl)
		mockDeployment = NewMockDeploymentInterface(ctrl)

		deploy = NewDeploy(&clients.Settings{
			AppsV1Interface: mockAppsV1,
		})
	})

	It("should fail if it fails to pull the deployment", func() {

		mockAppsV1.EXPECT().Deployments(ns).Return(mockDeployment)
		notFoundErr := k8serrors.NewNotFound(k8sschema.GroupResource{}, "")
		mockDeployment.EXPECT().Get(gomock.Any(), name, gomock.Any()).Return(nil, notFoundErr)

		err := deploy.WaitForReadyStatus(logLevel, name, ns, 1*time.Second)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to pull deployment"))
	})

	It("should fail if it times out", func() {

		returnedDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		}

		mockAppsV1.EXPECT().Deployments(ns).Return(mockDeployment).AnyTimes()
		mockDeployment.EXPECT().Get(gomock.Any(), name, gomock.Any()).Return(returnedDeployment, nil).AnyTimes()

		err := deploy.WaitForReadyStatus(logLevel, name, ns, 1*time.Second)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("timed out waiting for deployment"))
	})

	It("should return no error if the deployment gets ready in time", func() {

		returnedDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Replicas:      1,
			},
		}

		mockAppsV1.EXPECT().Deployments(ns).Return(mockDeployment).Times(3)
		mockDeployment.EXPECT().Get(gomock.Any(), name, gomock.Any()).Return(returnedDeployment, nil).Times(3)

		err := deploy.WaitForReadyStatus(logLevel, name, ns, 1*time.Second)
		Expect(err).NotTo(HaveOccurred())
	})
})
