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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// createMockConfigFunc returns a mock function for fetchConfigFunc
func createMockConfigFunc(mockResponse string, mockError error) FetchConfigFunc {
	return func(ctx context.Context, namespace, serviceName string) (string, error) {
		if mockError != nil {
			return "", mockError
		}
		return mockResponse, nil
	}
}

// DisabledExposerCreatorFunc is a function that always returns nil and doesn't create exposers
func DisabledExposerCreatorFunc(ctx context.Context, component *uiv1alpha1.ScalityUIComponent) error {
	// Do nothing - don't create exposers in this test
	return nil
}

// MockClient overrides some client methods for testing
type MockClient struct {
	client.Client
	getDeploymentWithReadyReplicas bool
}

// Get overrides the Get method to simulate a deployment with ready pods
func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	err := m.Client.Get(ctx, key, obj)

	// If the client is configured to return a deployment with ReadyReplicas
	// and the requested object is a deployment
	if m.getDeploymentWithReadyReplicas {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			deployment.Status.ReadyReplicas = 1
		}
	}

	return err
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
			if err != nil && errors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUIComponent{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "ui.scality.com/v1alpha1",
						Kind:       "ScalityUIComponent",
					},
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

			By("Cleanup the specific resource instance ScalityUIComponent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			// Also delete Deployment and Service
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, typeNamespacedName, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}

			service := &corev1.Service{}
			err = k8sClient.Get(ctx, typeNamespacedName, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}

			// Delete any created exposers
			exposerName := resourceName + "-exposer"
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: exposerName, Namespace: testNamespace}, exposer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, exposer)).To(Succeed())
			}

			// Delete any created ScalityUI resources
			scalityUI := &uiv1alpha1.ScalityUI{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-scality-ui", Namespace: testNamespace}, scalityUI)
			if err == nil {
				Expect(k8sClient.Delete(ctx, scalityUI)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client:                    k8sClient,
				Scheme:                    k8sClient.Scheme(),
				createOrUpdateExposerFunc: DisabledExposerCreatorFunc,
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

		It("should update status with component configuration when deployment is ready", func() {
			By("Creating a reconciler with a mock fetch config function")
			mockConfigResponse := `{
				"kind": "micro-app",
				"apiVersion": "v1",
				"metadata": {
					"kind": "AdminUI"
				},
				"spec": {
					"remoteEntryPath": "/remoteEntry.js",
					"publicPath": "/admin",
					"version": "1.0.0"
				}
			}`

			// Create a mocked client that returns deployments with ReadyReplicas=1
			mockClient := &MockClient{
				Client:                         k8sClient,
				getDeploymentWithReadyReplicas: true,
			}

			reconciler := &ScalityUIComponentReconciler{
				Client:                    mockClient,
				Scheme:                    k8sClient.Scheme(),
				fetchConfigFunc:           createMockConfigFunc(mockConfigResponse, nil),
				createOrUpdateExposerFunc: DisabledExposerCreatorFunc,
			}

			// Create the base deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  resourceName,
									Image: testImage,
								},
							},
						},
					},
				},
			}

			// Create the deployment without status - the mock will handle injecting ReadyReplicas=1
			_, err := ctrl.CreateOrUpdate(ctx, k8sClient, deployment, func() error {
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			// Run the reconciliation
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the status was updated
			updatedComponent := &uiv1alpha1.ScalityUIComponent{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedComponent)
				if err != nil {
					return false
				}
				return updatedComponent.Status.Kind == "AdminUI" &&
					updatedComponent.Status.PublicPath == "/admin" &&
					updatedComponent.Status.Version == "1.0.0"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			// Verify the status condition
			condition := meta.FindStatusCondition(updatedComponent.Status.Conditions, "ConfigurationRetrieved")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal("FetchSucceeded"))
		})

		It("should set failure condition when cannot fetch configuration", func() {
			By("Creating a reconciler with a mock fetch config function that fails")
			// Create a mocked client that returns deployments with ReadyReplicas=1
			mockClient := &MockClient{
				Client:                         k8sClient,
				getDeploymentWithReadyReplicas: true,
			}

			reconciler := &ScalityUIComponentReconciler{
				Client:                    mockClient,
				Scheme:                    k8sClient.Scheme(),
				fetchConfigFunc:           createMockConfigFunc("", fmt.Errorf("failed to connect to service")),
				createOrUpdateExposerFunc: DisabledExposerCreatorFunc,
			}

			// Create the base deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  resourceName,
									Image: testImage,
								},
							},
						},
					},
				},
			}

			// Create the deployment without status - the mock will handle injecting ReadyReplicas=1
			_, err := ctrl.CreateOrUpdate(ctx, k8sClient, deployment, func() error {
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			// Run the reconciliation
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// Should succeed but with a requeue
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 10))

			// Verify the failure condition
			updatedComponent := &uiv1alpha1.ScalityUIComponent{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedComponent)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedComponent.Status.Conditions, "ConfigurationRetrieved")
				return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "FetchFailed"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())
		})

		It("should set failure condition when configuration JSON is invalid", func() {
			By("Creating a reconciler with a mock fetch config function that returns invalid JSON")
			// Create a mocked client that returns deployments with ReadyReplicas=1
			mockClient := &MockClient{
				Client:                         k8sClient,
				getDeploymentWithReadyReplicas: true,
			}

			reconciler := &ScalityUIComponentReconciler{
				Client:                    mockClient,
				Scheme:                    k8sClient.Scheme(),
				fetchConfigFunc:           createMockConfigFunc(`{"invalid": "json`, nil),
				createOrUpdateExposerFunc: DisabledExposerCreatorFunc,
			}

			// Create the base deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  resourceName,
									Image: testImage,
								},
							},
						},
					},
				},
			}

			// Create the deployment without status - the mock will handle injecting ReadyReplicas=1
			_, err := ctrl.CreateOrUpdate(ctx, k8sClient, deployment, func() error {
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			// Run the reconciliation
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// Should succeed but with a requeue
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Second * 10))

			// Verify the parsing failure condition
			updatedComponent := &uiv1alpha1.ScalityUIComponent{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedComponent)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedComponent.Status.Conditions, "ConfigurationRetrieved")
				return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "ParseFailed"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())
		})

		It("should automatically create a ScalityUIComponentExposer when component is ready", func() {
			By("Creating a ScalityUI resource first")

			// Create a ScalityUI resource that the exposer will reference
			scalityUI := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-scality-ui",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/shell-ui:latest",
					ProductName: "TEST",
				},
			}

			Expect(k8sClient.Create(ctx, scalityUI)).To(Succeed())

			By("Setting up a reconciler with mock config")
			mockConfigResponse := `{
				"kind": "micro-app",
				"apiVersion": "v1",
				"metadata": {
					"kind": "TestUI"
				},
				"spec": {
					"remoteEntryPath": "/remoteEntry.js",
					"publicPath": "/test",
					"version": "1.0.0"
				}
			}`

			// Create a mocked client that returns deployments with ReadyReplicas=1
			mockClient := &MockClient{
				Client:                         k8sClient,
				getDeploymentWithReadyReplicas: true,
			}

			reconciler := &ScalityUIComponentReconciler{
				Client:          mockClient,
				Scheme:          k8sClient.Scheme(),
				fetchConfigFunc: createMockConfigFunc(mockConfigResponse, nil),
			}

			// Get the existing ScalityUIComponent with proper TypeMeta
			existingComponent := &uiv1alpha1.ScalityUIComponent{}
			err := k8sClient.Get(ctx, typeNamespacedName, existingComponent)
			Expect(err).NotTo(HaveOccurred())

			// Make sure TypeMeta is set
			existingComponent.APIVersion = "ui.scality.com/v1alpha1"
			existingComponent.Kind = "ScalityUIComponent"
			Expect(k8sClient.Update(ctx, existingComponent)).To(Succeed())

			// Create the base deployment to simulate a ready component
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  resourceName,
									Image: testImage,
								},
							},
						},
					},
				},
			}

			_, err = ctrl.CreateOrUpdate(ctx, k8sClient, deployment, func() error {
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("Running the reconciliation")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the ScalityUIComponentExposer was created")
			expectedExposerName := resourceName + "-exposer"
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      expectedExposerName,
					Namespace: testNamespace,
				}, exposer)
				return err == nil
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			By("Verifying the exposer references the correct ScalityUI and ScalityUIComponent")
			Expect(exposer.Spec.ScalityUI).To(Equal("test-scality-ui"))
			Expect(exposer.Spec.ScalityUIComponent).To(Equal(resourceName))

			By("Verifying the exposer has the ScalityUIComponent as its owner")
			Expect(exposer.OwnerReferences).To(HaveLen(1))
			Expect(exposer.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(exposer.OwnerReferences[0].Kind).To(Equal("ScalityUIComponent"))
			Expect(*exposer.OwnerReferences[0].Controller).To(BeTrue())
		})

		It("should not create an exposer when no ScalityUI is available", func() {
			// First delete any existing ScalityUI resources
			scalityUIList := &uiv1alpha1.ScalityUIList{}
			Expect(k8sClient.List(ctx, scalityUIList, client.InNamespace(testNamespace))).To(Succeed())

			for _, ui := range scalityUIList.Items {
				Expect(k8sClient.Delete(ctx, &ui)).To(Succeed())
			}

			By("Setting up a reconciler with mock config")
			mockConfigResponse := `{
				"kind": "micro-app",
				"apiVersion": "v1",
				"metadata": {
					"kind": "TestUI"
				},
				"spec": {
					"remoteEntryPath": "/remoteEntry.js",
					"publicPath": "/test",
					"version": "1.0.0"
				}
			}`

			// Create a mocked client that returns deployments with ReadyReplicas=1
			mockClient := &MockClient{
				Client:                         k8sClient,
				getDeploymentWithReadyReplicas: true,
			}

			reconciler := &ScalityUIComponentReconciler{
				Client:          mockClient,
				Scheme:          k8sClient.Scheme(),
				fetchConfigFunc: createMockConfigFunc(mockConfigResponse, nil),
			}

			// Create the base deployment to simulate a ready component
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  resourceName,
									Image: testImage,
								},
							},
						},
					},
				},
			}

			_, err := ctrl.CreateOrUpdate(ctx, k8sClient, deployment, func() error {
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("Running the reconciliation")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})

			By("Verifying the reconciliation failed because no ScalityUI was found")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no ScalityUI found in namespace"))

			By("Verifying no ScalityUIComponentExposer was created")
			expectedExposerName := resourceName + "-exposer"
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}

			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      expectedExposerName,
				Namespace: testNamespace,
			}, exposer)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
