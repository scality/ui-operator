/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// MockConfigFetcher is a mock implementation of the ConfigFetcher interface
type MockConfigFetcher struct {
	ShouldFail    bool
	ErrorMessage  string
	ConfigContent string
	ReceivedCalls []MockFetchCall
}

type MockFetchCall struct {
	Namespace   string
	ServiceName string
	Port        int
}

func (m *MockConfigFetcher) FetchConfig(ctx context.Context, namespace, serviceName string, port int) (string, error) {
	call := MockFetchCall{
		Namespace:   namespace,
		ServiceName: serviceName,
		Port:        port,
	}
	m.ReceivedCalls = append(m.ReceivedCalls, call)

	if m.ShouldFail {
		return "", errors.New(m.ErrorMessage)
	}
	return m.ConfigContent, nil
}

var _ = Describe("ScalityUIComponent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const testNamespace = "default"
		const testImage = "scality/ui-component:latest"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		scalityuicomponent := &uiv1alpha1.ScalityUIComponent{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ScalityUIComponent")
			err := k8sClient.Get(ctx, typeNamespacedName, scalityuicomponent)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUIComponent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: uiv1alpha1.ScalityUIComponentSpec{
						Image: testImage,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &uiv1alpha1.ScalityUIComponent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup associated Deployment and Service as well
			deployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, typeNamespacedName, deployment) // Ignore error if not found
			if deployment.Name != "" {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}

			service := &corev1.Service{}
			_ = k8sClient.Get(ctx, typeNamespacedName, service) // Ignore error if not found
			if service.Name != "" {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}

			// Cleanup associated ScalityUIComponentExposer
			exposerName := fmt.Sprintf("%s-exposer", resourceName)
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: exposerName, Namespace: testNamespace}, exposer)
			if exposer.Name != "" {
				Expect(k8sClient.Delete(ctx, exposer)).To(Succeed())
			}

			By("Cleanup the specific resource instance ScalityUIComponent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Deployment was created with correct specifications")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, typeNamespacedName, deployment)
			Expect(err).NotTo(HaveOccurred())

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(testImage))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal(resourceName))

			Expect(deployment.Spec.Template.ObjectMeta.Labels["app"]).To(Equal(resourceName))
			Expect(deployment.Spec.Selector.MatchLabels["app"]).To(Equal(resourceName))

			By("Checking if Service was created with correct specifications")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, typeNamespacedName, service)
			Expect(err).NotTo(HaveOccurred())

			Expect(service.Spec.Selector["app"]).To(Equal(resourceName))
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).To(Equal("http"))
			Expect(service.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(80)))
		})

		It("should create ScalityUIComponentExposer for the component", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Making deployment ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Triggering reconciliation again")
			// Create a mock server to handle the config fetch
			testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"kind": "UIModule", 
					"apiVersion": "v1alpha1", 
					"metadata": {"kind": "TestKind"}, 
					"spec": {
						"remoteEntryPath": "/remoteEntry.js", 
						"publicPath": "/test-public/", 
						"version": "1.2.3"
					}
				}`))
			}))
			defer testServer.Close()

			testRESTConfig := rest.CopyConfig(cfg)
			parsedURL, err := url.Parse(testServer.URL)
			Expect(err).NotTo(HaveOccurred())
			testRESTConfig.Host = parsedURL.Host
			testRESTConfig.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
			testRESTConfig.BearerToken = ""

			controllerReconciler.Config = testRESTConfig

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if ScalityUIComponentExposer was created with correct specifications")
			exposerName := fmt.Sprintf("%s-exposer", resourceName)
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      exposerName,
				Namespace: testNamespace,
			}, exposer)
			Expect(err).NotTo(HaveOccurred())

			// Verify exposer spec
			Expect(exposer.Spec.ScalityUI).To(Equal(DefaultScalityUIName))
			Expect(exposer.Spec.ScalityUIComponent).To(Equal(resourceName))

			// Verify owner reference is set correctly
			Expect(exposer.OwnerReferences).To(HaveLen(1))
			Expect(exposer.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(exposer.OwnerReferences[0].Kind).To(Equal("ScalityUIComponent"))
		})

		It("should requeue if Deployment is not ready", func() {
			By("Reconciling the created resource")
			mockFetcher := &MockConfigFetcher{}
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Deployment and Service are created by the reconcile loop

			By("Ensuring Deployment exists")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			By("Updating Deployment status to not ready")
			deployment.Status.ReadyReplicas = 0
			deployment.Status.Replicas = 1 // Ensure some replicas are desired
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			// Second reconcile, now Deployment is fetched and checked for readiness
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 10))

			// Check that no status condition for ConfigurationRetrieved was added yet
			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).To(BeNil())

			Expect(mockFetcher.ReceivedCalls).To(BeEmpty()) // Ensure fetcher was not called
		})

		It("should set ConfigurationRetrieved=False condition if config fetch fails", func() {
			By("Creating a mock config fetcher that fails")
			mockFetcher := &MockConfigFetcher{
				ShouldFail:   true,
				ErrorMessage: "Mock fetching error",
			}

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			// First reconcile to create Deployment and Service
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Ensuring Deployment exists and making it ready")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, deployment)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			By("Triggering Reconcile again for config fetch logic")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred()) // The reconcile itself should not error, but requeue
			Expect(result.RequeueAfter).To(Equal(time.Second * 10))

			By("Checking ScalityUIComponent status conditions")
			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("FetchFailed"))
			Expect(cond.Message).To(ContainSubstring("Failed to fetch configuration"))

			By("Verifying that the mock was called with correct parameters")
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1))
			Expect(mockFetcher.ReceivedCalls[0].Namespace).To(Equal(testNamespace))
			Expect(mockFetcher.ReceivedCalls[0].ServiceName).To(Equal(resourceName))
			Expect(mockFetcher.ReceivedCalls[0].Port).To(Equal(DefaultServicePort))
		})

		It("should set ConfigurationRetrieved=True and update status if config fetch and parse succeed", func() {
			By("Creating a mock config fetcher with successful response")
			mockFetcher := &MockConfigFetcher{
				ShouldFail: false,
				ConfigContent: `{
					"kind": "UIModule", 
					"apiVersion": "v1alpha1", 
					"metadata": {"kind": "TestKind"}, 
					"spec": {
						"remoteEntryPath": "/remoteEntry.js", 
						"publicPath": "/test-public/", 
						"version": "1.2.3"
					}
				}`,
			}

			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, typeNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())
			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())
			Expect(updatedScalityUIComponent.Status.Kind).To(Equal("TestKind"))
			Expect(updatedScalityUIComponent.Status.PublicPath).To(Equal("/test-public/"))
			Expect(updatedScalityUIComponent.Status.Version).To(Equal("1.2.3"))

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("FetchSucceeded"))
			Expect(cond.Message).To(Equal("Successfully fetched and applied UI component configuration"))

			By("Verifying that the mock was called with correct parameters")
			Expect(mockFetcher.ReceivedCalls).To(HaveLen(1))
			Expect(mockFetcher.ReceivedCalls[0].Namespace).To(Equal(testNamespace))
			Expect(mockFetcher.ReceivedCalls[0].ServiceName).To(Equal(resourceName))
		})

		It("should set ConfigurationRetrieved=False with ParseFailed reason if config parse fails", func() {
			By("Creating a mock config fetcher with malformed JSON")
			mockFetcher := &MockConfigFetcher{
				ShouldFail:    false,
				ConfigContent: `{"metadata": {"kind": "TestKind"}, "spec": {"publicPath": "/test/", "version": "1.2.3"}, MALFORMED`,
			}

			controllerReconciler := &ScalityUIComponentReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ConfigFetcher: mockFetcher,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, typeNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())
			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 10))

			updatedScalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedScalityUIComponent)).To(Succeed())

			cond := meta.FindStatusCondition(updatedScalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ParseFailed"))
			Expect(cond.Message).To(ContainSubstring("Failed to parse configuration:"))
		})
	})
})
