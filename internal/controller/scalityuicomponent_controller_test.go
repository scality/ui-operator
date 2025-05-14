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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"k8s.io/client-go/rest"
)

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
			if err != nil && errors.IsNotFound(err) {
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

		It("should requeue if Deployment is not ready", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: cfg, // cfg should be available from test setup (suite_test.go)
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
		})

		It("should set ConfigurationRetrieved=False condition if config fetch fails", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: cfg, // cfg should be available from test setup (suite_test.go)
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

			// Ensure service exists, as fetchMicroAppConfig will try to proxy through it
			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, service)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

			By("Triggering Reconcile again for config fetch logic")
			// In envtest, the service proxy will likely fail as no real pods are running.
			// This simulates fetchMicroAppConfig returning an error.
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
		})

		It("should set ConfigurationRetrieved=True and update status if config fetch and parse succeed", func() {
			testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:80/proxy/.well-known/micro-app-configuration", testNamespace, resourceName)
				Expect(r.URL.Path).To(Equal(expectedPath))

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

			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: testRESTConfig,
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, typeNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())
			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			Eventually(func() error { // Ensure service is also there
				service := &corev1.Service{}
				return k8sClient.Get(ctx, typeNamespacedName, service)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

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
		})

		It("should set ConfigurationRetrieved=False with ParseFailed reason if config parse fails", func() {
			testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:80/proxy/.well-known/micro-app-configuration", testNamespace, resourceName)
				Expect(r.URL.Path).To(Equal(expectedPath))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"metadata": {"kind": "TestKind"}, "spec": {"publicPath": "/test/", "version": "1.2.3"}, MALFORMED`))
			}))
			defer testServer.Close()

			testRESTConfig := rest.CopyConfig(cfg)
			parsedURL, err := url.Parse(testServer.URL)
			Expect(err).NotTo(HaveOccurred())
			testRESTConfig.Host = parsedURL.Host
			testRESTConfig.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
			testRESTConfig.BearerToken = ""

			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: testRESTConfig,
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Eventually(func() error { return k8sClient.Get(ctx, typeNamespacedName, deployment) }, time.Second*5, time.Millisecond*250).Should(Succeed())
			deployment.Status.ReadyReplicas = 1
			deployment.Status.Replicas = 1
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			Eventually(func() error { // Ensure service is also there
				service := &corev1.Service{}
				return k8sClient.Get(ctx, typeNamespacedName, service)
			}, time.Second*5, time.Millisecond*250).Should(Succeed())

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
