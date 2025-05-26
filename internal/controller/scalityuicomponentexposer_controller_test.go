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
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

var _ = Describe("ScalityUIComponentExposer Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			exposerName   = "test-exposer"
			uiName        = "test-ui"
			componentName = "test-component"
			testNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      exposerName,
			Namespace: testNamespace,
		}

		BeforeEach(func() {
			// Create ScalityUI resource
			ui := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      uiName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			// Create ScalityUIComponent resource
			component := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      componentName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image: "scality/component:latest",
				},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			// Create test deployment for the component
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      componentName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": componentName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": componentName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "main",
									Image: "scality/component:latest",
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 80,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		})

		AfterEach(func() {
			// Clean up resources
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			_ = k8sClient.Get(ctx, typeNamespacedName, exposer)
			_ = k8sClient.Delete(ctx, exposer)

			ui := &uiv1alpha1.ScalityUI{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: uiName, Namespace: testNamespace}, ui)
			_ = k8sClient.Delete(ctx, ui)

			component := &uiv1alpha1.ScalityUIComponent{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, component)
			_ = k8sClient.Delete(ctx, component)

			// Clean up any test deployments
			deployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, deployment)
			_ = k8sClient.Delete(ctx, deployment)

			// Clean up any test configmaps
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: testNamespace}, configMap)
			_ = k8sClient.Delete(ctx, configMap)
		})

		It("should successfully reconcile the resource with default configuration", func() {
			By("Creating the custom resource for the Kind ScalityUIComponentExposer")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			By("Verifying ConfigMap content")
			Expect(configMap.Data).To(HaveKey(configMapKey))

			var runtimeConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data[configMapKey]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(runtimeConfig.Kind).To(Equal("MicroAppRuntimeConfiguration"))
			Expect(runtimeConfig.APIVersion).To(Equal("ui.scality.com/v1alpha1"))
			Expect(runtimeConfig.Metadata.Kind).To(Equal(""))
			Expect(runtimeConfig.Metadata.Name).To(Equal(exposerName))
			Expect(runtimeConfig.Spec.ScalityUI).To(Equal(uiName))
			Expect(runtimeConfig.Spec.ScalityUIComponent).To(Equal(componentName))

			// Verify default auth configuration
			authConfig := runtimeConfig.Spec.Auth
			Expect(authConfig["kind"]).To(Equal("OIDC"))
			Expect(authConfig["redirectUrl"]).To(Equal("/"))
			Expect(authConfig["responseType"]).To(Equal("code"))
			Expect(authConfig["scopes"]).To(Equal("openid email profile"))
			Expect(authConfig["providerLogout"]).To(Equal(true))

			By("Checking status conditions")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			configMapCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "ConfigMapReady")
			Expect(configMapCondition).NotTo(BeNil())
			Expect(configMapCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(configMapCondition.Reason).To(Equal("ReconcileSucceeded"))
		})

		It("should handle custom configuration", func() {
			By("Creating the custom resource with custom configuration")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
					Auth: &uiv1alpha1.AuthConfig{
						Kind:        "OIDC",
						ProviderURL: "https://auth.example.com",
						ClientID:    "test-client",
					},
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying custom configuration in ConfigMap")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			var runtimeConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data[configMapKey]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(runtimeConfig.Metadata.Name).To(Equal(exposerName)) // exposer.Name

			authConfig := runtimeConfig.Spec.Auth
			Expect(authConfig["kind"]).To(Equal("OIDC"))
			Expect(authConfig["providerUrl"]).To(Equal("https://auth.example.com"))
			Expect(authConfig["clientId"]).To(Equal("test-client"))
		})

		It("should handle missing dependencies", func() {
			By("Creating the custom resource with non-existent dependencies")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          "non-existent-ui",
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Minute))

			By("Checking dependency error condition")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			depCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "DependenciesReady")
			Expect(depCondition).NotTo(BeNil())
			Expect(depCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCondition.Reason).To(Equal("DependencyMissing"))
		})

		It("should handle missing component dependency", func() {
			By("Creating the custom resource with non-existent component")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: "non-existent-component",
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Minute))

			By("Checking dependency error condition")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			depCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "DependenciesReady")
			Expect(depCondition).NotTo(BeNil())
			Expect(depCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCondition.Reason).To(Equal("DependencyMissing"))
			Expect(depCondition.Message).To(ContainSubstring("ScalityUIComponent \"non-existent-component\" not found"))
		})

		It("should handle partial auth configuration with kubebuilder defaults", func() {
			By("Creating the custom resource with partial auth configuration")
			providerLogout := false
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
					Auth: &uiv1alpha1.AuthConfig{
						ProviderURL:    "https://auth.example.com",
						ClientID:       "test-client",
						ProviderLogout: &providerLogout,
						// Note: Kind, RedirectURL, ResponseType, Scopes should get defaults
					},
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying partial configuration with defaults in ConfigMap")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			var runtimeConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data[configMapKey]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			authConfig := runtimeConfig.Spec.Auth
			Expect(authConfig["providerUrl"]).To(Equal("https://auth.example.com"))
			Expect(authConfig["clientId"]).To(Equal("test-client"))
			Expect(authConfig["providerLogout"]).To(Equal(false))
			// These should be from kubebuilder defaults when auth object is provided but fields are empty
			Expect(authConfig["kind"]).To(Equal("OIDC"))
			Expect(authConfig["redirectUrl"]).To(Equal("/"))
			Expect(authConfig["responseType"]).To(Equal("code"))
			Expect(authConfig["scopes"]).To(Equal("openid email profile"))
		})

		It("should update ConfigMap when exposer spec changes", func() {
			By("Creating the initial custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating the exposer with auth configuration")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			updatedExposer.Spec.Auth = &uiv1alpha1.AuthConfig{
				Kind:        "OIDC",
				ProviderURL: "https://updated-auth.example.com",
				ClientID:    "updated-client",
			}
			Expect(k8sClient.Update(ctx, updatedExposer)).To(Succeed())

			By("Reconciling the updated resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap was updated")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
				if err != nil {
					return false
				}

				var runtimeConfig MicroAppRuntimeConfiguration
				err = json.Unmarshal([]byte(configMap.Data[configMapKey]), &runtimeConfig)
				if err != nil {
					return false
				}

				authConfig := runtimeConfig.Spec.Auth
				return authConfig["providerUrl"] == "https://updated-auth.example.com" &&
					authConfig["clientId"] == "updated-client"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())
		})

		It("should set correct owner reference on ConfigMap", func() {
			By("Creating the custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap owner reference")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(configMap.OwnerReferences).To(HaveLen(1))
			ownerRef := configMap.OwnerReferences[0]
			Expect(ownerRef.APIVersion).To(Equal("ui.scality.com/v1alpha1"))
			Expect(ownerRef.Kind).To(Equal("ScalityUIComponentExposer"))
			Expect(ownerRef.Name).To(Equal(exposerName))
			Expect(*ownerRef.Controller).To(BeTrue())
			Expect(*ownerRef.BlockOwnerDeletion).To(BeTrue())
		})

		It("should handle resource not found gracefully", func() {
			By("Reconciling a non-existent resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent-exposer",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should calculate ConfigMap hash correctly", func() {
			By("Creating the custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap hash calculation")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			// Test hash calculation
			hash, err := controllerReconciler.calculateConfigMapHash(configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).NotTo(BeEmpty())
			Expect(len(hash)).To(Equal(64)) // SHA-256 hex string length

			// Verify hash is deterministic
			hash2, err := controllerReconciler.calculateConfigMapHash(configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(hash).To(Equal(hash2))
		})

		It("should update deployment with ConfigMap volume mount", func() {
			deploymentName := componentName + "-deploy-test"
			By("Creating a test deployment")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": deploymentName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": deploymentName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Create a component that references this deployment
			testComponent := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image: "scality/component:latest",
				},
			}
			Expect(k8sClient.Create(ctx, testComponent)).To(Succeed())

			By("Creating the custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName + "-deploy",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: deploymentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposerName + "-deploy",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment was updated with ConfigMap volume")
			updatedDeployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: testNamespace,
				}, updatedDeployment)
				if err != nil {
					return false
				}

				// Check if ConfigMap volume was added
				expectedVolumeName := volumeNamePrefix + deploymentName
				for _, volume := range updatedDeployment.Spec.Template.Spec.Volumes {
					if volume.Name == expectedVolumeName {
						return volume.ConfigMap != nil &&
							volume.ConfigMap.Name == deploymentName+"-runtime-app-configuration"
					}
				}
				return false
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			By("Verifying container has ConfigMap volume mount")
			Expect(updatedDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := updatedDeployment.Spec.Template.Spec.Containers[0]

			expectedVolumeName := volumeNamePrefix + deploymentName
			var foundMount *corev1.VolumeMount
			for i, mount := range container.VolumeMounts {
				if mount.Name == expectedVolumeName {
					foundMount = &container.VolumeMounts[i]
					break
				}
			}

			Expect(foundMount).NotTo(BeNil())
			Expect(foundMount.MountPath).To(Equal(mountPath))
			Expect(foundMount.SubPath).To(Equal(configMapKey))
			Expect(foundMount.ReadOnly).To(BeTrue())

			By("Verifying config hash annotation was added")
			Expect(updatedDeployment.Spec.Template.Annotations).To(HaveKey(configHashAnnotation))

			By("Cleaning up test resources")
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			Expect(k8sClient.Delete(ctx, testComponent)).To(Succeed())
			Expect(k8sClient.Delete(ctx, exposer)).To(Succeed())
		})

		It("should trigger pod restart when ConfigMap content changes", func() {
			deploymentName := componentName + "-restart-test"
			By("Creating a test deployment")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": deploymentName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": deploymentName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Create a component that references this deployment
			testComponent := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image: "scality/component:latest",
				},
			}
			Expect(k8sClient.Create(ctx, testComponent)).To(Succeed())

			By("Creating the custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName + "-restart",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: deploymentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposerName + "-restart",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial hash annotation")
			updatedDeployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: testNamespace,
				}, updatedDeployment)
				return err == nil && updatedDeployment.Spec.Template.Annotations[configHashAnnotation] != ""
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			initialHash := updatedDeployment.Spec.Template.Annotations[configHashAnnotation]

			By("Updating the exposer configuration")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      exposerName + "-restart",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			updatedExposer.Spec.Auth = &uiv1alpha1.AuthConfig{
				Kind:        "OIDC",
				ProviderURL: "https://changed-auth.example.com",
				ClientID:    "changed-client",
			}
			Expect(k8sClient.Update(ctx, updatedExposer)).To(Succeed())

			By("Reconciling the updated resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposerName + "-restart",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying hash annotation changed to trigger pod restart")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: testNamespace,
				}, updatedDeployment)
				if err != nil {
					return false
				}

				newHash := updatedDeployment.Spec.Template.Annotations[configHashAnnotation]
				return newHash != "" && newHash != initialHash
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			By("Cleaning up test resources")
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			Expect(k8sClient.Delete(ctx, testComponent)).To(Succeed())
			Expect(k8sClient.Delete(ctx, exposer)).To(Succeed())
		})

		It("should handle deployment not found gracefully", func() {
			By("Creating the custom resource without corresponding deployment")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment ready condition is still true")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			deploymentCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "DeploymentReady")
			Expect(deploymentCondition).NotTo(BeNil())
			Expect(deploymentCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(deploymentCondition.Reason).To(Equal("ReconcileSucceeded"))
		})

		It("should add management annotations to ConfigMap", func() {
			By("Creating the custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap has management annotations")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(configMap.Annotations).To(HaveKey("ui.scality.com/managed-by"))
			Expect(configMap.Annotations["ui.scality.com/managed-by"]).To(Equal("scalityuicomponentexposer-controller"))
			Expect(configMap.Annotations).To(HaveKey("ui.scality.com/last-updated"))
			Expect(configMap.Annotations["ui.scality.com/last-updated"]).NotTo(BeEmpty())
		})

		It("should preserve existing volumes and mounts when adding ConfigMap mount", func() {
			deploymentName := componentName + "-preserve-test"
			By("Creating a test deployment with existing volumes")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": deploymentName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": deploymentName,
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "existing-volume",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "existing-volume",
											MountPath: "/existing",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Create a component that references this deployment
			testComponent := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image: "scality/component:latest",
				},
			}
			Expect(k8sClient.Create(ctx, testComponent)).To(Succeed())

			By("Creating the custom resource")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      exposerName + "-preserve",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: deploymentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposerName + "-preserve",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying existing volumes and mounts are preserved")
			updatedDeployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: testNamespace,
				}, updatedDeployment)
				return err == nil && len(updatedDeployment.Spec.Template.Spec.Volumes) >= 2
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			// Check existing volume is preserved
			var existingVolumeFound bool
			var configVolumeFound bool
			expectedConfigVolumeName := volumeNamePrefix + deploymentName

			for _, volume := range updatedDeployment.Spec.Template.Spec.Volumes {
				if volume.Name == "existing-volume" {
					existingVolumeFound = true
					Expect(volume.EmptyDir).NotTo(BeNil())
				}
				if volume.Name == expectedConfigVolumeName {
					configVolumeFound = true
					Expect(volume.ConfigMap).NotTo(BeNil())
				}
			}

			Expect(existingVolumeFound).To(BeTrue())
			Expect(configVolumeFound).To(BeTrue())

			// Check existing mount is preserved and new mount is added
			container := updatedDeployment.Spec.Template.Spec.Containers[0]
			var existingMountFound bool
			var configMountFound bool

			for _, mount := range container.VolumeMounts {
				if mount.Name == "existing-volume" {
					existingMountFound = true
					Expect(mount.MountPath).To(Equal("/existing"))
				}
				if mount.Name == expectedConfigVolumeName {
					configMountFound = true
					Expect(mount.MountPath).To(Equal(mountPath))
				}
			}

			Expect(existingMountFound).To(BeTrue())
			Expect(configMountFound).To(BeTrue())

			By("Cleaning up test resources")
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			Expect(k8sClient.Delete(ctx, testComponent)).To(Succeed())
			Expect(k8sClient.Delete(ctx, exposer)).To(Succeed())
		})
	})
})

// Helper function to create int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
