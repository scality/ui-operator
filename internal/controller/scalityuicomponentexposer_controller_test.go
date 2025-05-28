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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
			// Create ScalityUI resource with default auth
			ui := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: uiName,
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
		})

		AfterEach(func() {
			// Clean up resources
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			_ = k8sClient.Get(ctx, typeNamespacedName, exposer)
			_ = k8sClient.Delete(ctx, exposer)

			ui := &uiv1alpha1.ScalityUI{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: uiName}, ui)
			_ = k8sClient.Delete(ctx, ui)

			component := &uiv1alpha1.ScalityUIComponent{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, component)
			_ = k8sClient.Delete(ctx, component)
		})

		It("should successfully reconcile the resource with no auth configuration", func() {
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

			// Verify no auth configuration since ScalityUI doesn't have auth and exposer doesn't specify auth
			authConfig := runtimeConfig.Spec.Auth
			Expect(authConfig).To(BeEmpty())

			By("Checking status conditions")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			configMapCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "ConfigMapReady")
			Expect(configMapCondition).NotTo(BeNil())
			Expect(configMapCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(configMapCondition.Reason).To(Equal("ReconcileSucceeded"))
		})

		It("should handle no auth when ScalityUI has no auth", func() {
			By("Creating a ScalityUI without auth configuration")
			uiNoAuth := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ui-no-auth",
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
					// No Auth field
				},
			}
			Expect(k8sClient.Create(ctx, uiNoAuth)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, uiNoAuth)
			}()

			By("Creating a separate component for this test")
			componentNoAuth := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-no-auth",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image: "scality/component:latest",
				},
			}
			Expect(k8sClient.Create(ctx, componentNoAuth)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, componentNoAuth)
			}()

			By("Creating the custom resource referencing UI without auth")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-no-auth",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          "test-ui-no-auth",
					ScalityUIComponent: "test-component-no-auth",
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, exposer)
			}()

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-no-auth",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying empty auth configuration")
			configMap := &corev1.ConfigMap{}
			configMapName := "test-component-no-auth-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			var runtimeConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data[configMapKey]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			// Verify empty auth configuration when neither exposer nor ScalityUI has auth
			authConfig := runtimeConfig.Spec.Auth
			Expect(authConfig).To(BeEmpty())
		})

		It("should handle complete custom auth configuration", func() {
			By("Creating the custom resource with complete auth configuration")
			providerLogout := true
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
						Kind:           "OIDC",
						ProviderURL:    "https://auth.example.com",
						RedirectURL:    "/callback",
						ClientID:       "test-client",
						ResponseType:   "code",
						Scopes:         "openid profile email",
						ProviderLogout: &providerLogout,
					},
					SelfConfiguration: &runtime.RawExtension{
						Raw: []byte(`{"url": "/test"}`),
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

			By("Verifying complete auth configuration in ConfigMap")
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

			Expect(runtimeConfig.Metadata.Name).To(Equal(exposerName))

			authConfig := runtimeConfig.Spec.Auth
			Expect(authConfig["kind"]).To(Equal("OIDC"))
			Expect(authConfig["providerUrl"]).To(Equal("https://auth.example.com"))
			Expect(authConfig["redirectUrl"]).To(Equal("/callback"))
			Expect(authConfig["clientId"]).To(Equal("test-client"))
			Expect(authConfig["responseType"]).To(Equal("code"))
			Expect(authConfig["scopes"]).To(Equal("openid profile email"))
			Expect(authConfig["providerLogout"]).To(Equal(true))

			selfConfig := runtimeConfig.Spec.SelfConfiguration
			Expect(selfConfig["url"]).To(Equal("/test"))
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

		It("should validate auth configuration correctly", func() {
			By("Testing the validation logic directly")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Testing incomplete auth config")
			incompleteAuth := &uiv1alpha1.AuthConfig{
				ProviderURL: "https://auth.example.com",
				ClientID:    "test-client",
				// Missing: Kind, RedirectURL, ResponseType, Scopes
			}

			err := controllerReconciler.validateAuthConfig(incompleteAuth)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is required"))

			By("Testing complete auth config")
			completeAuth := &uiv1alpha1.AuthConfig{
				Kind:         "OIDC",
				ProviderURL:  "https://auth.example.com",
				RedirectURL:  "/callback",
				ClientID:     "test-client",
				ResponseType: "code",
				Scopes:       "openid profile email",
			}

			err = controllerReconciler.validateAuthConfig(completeAuth)
			Expect(err).NotTo(HaveOccurred())
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

			By("Updating the exposer with complete auth configuration")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedExposer)).To(Succeed())

			providerLogout := false
			updatedExposer.Spec.Auth = &uiv1alpha1.AuthConfig{
				Kind:           "OIDC",
				ProviderURL:    "https://updated-auth.example.com",
				RedirectURL:    "/updated-callback",
				ClientID:       "updated-client",
				ResponseType:   "code",
				Scopes:         "openid profile",
				ProviderLogout: &providerLogout,
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

		It("should test Watch mechanism and auth validation", func() {
			By("Testing findExposersForScalityUI function")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Test with existing UI
			ui := &uiv1alpha1.ScalityUI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uiName, Namespace: testNamespace}, ui)).To(Succeed())

			requests := controllerReconciler.findExposersForScalityUI(ctx, ui)
			Expect(requests).To(HaveLen(0)) // No exposers created yet

			By("Testing incomplete auth validation")
			incompleteAuth := &uiv1alpha1.AuthConfig{
				Kind:        "OIDC",
				ProviderURL: "https://auth.example.com",
				// Missing required fields
			}
			err := controllerReconciler.validateAuthConfig(incompleteAuth)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is required"))

			By("Testing complete auth validation")
			completeAuth := &uiv1alpha1.AuthConfig{
				Kind:         "OIDC",
				ProviderURL:  "https://auth.example.com",
				RedirectURL:  "/callback",
				ClientID:     "test-client",
				ResponseType: "code",
				Scopes:       "openid profile",
			}
			err = controllerReconciler.validateAuthConfig(completeAuth)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
