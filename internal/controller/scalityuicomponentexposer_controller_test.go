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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

		// Helper functions to reduce repetition
		updateComponentStatus := func(component *uiv1alpha1.ScalityUIComponent, publicPath string, kind ...string) {
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: component.Name, Namespace: testNamespace}, component); err != nil {
					return err
				}
				component.Status.PublicPath = publicPath
				if len(kind) > 0 {
					component.Status.Kind = kind[0]
				}
				return k8sClient.Status().Update(ctx, component)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
		}

		createComponent := func(name, publicPath string, mountPath ...string) *uiv1alpha1.ScalityUIComponent {
			path := "/usr/share/nginx/html/.well-known"
			if len(mountPath) > 0 {
				path = mountPath[0]
			}

			component := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/component:latest",
					MountPath: path,
				},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())

			if publicPath != "" {
				updateComponentStatus(component, publicPath)
			}
			return component
		}

		createScalityUI := func(name string, networks *uiv1alpha1.UINetworks) *uiv1alpha1.ScalityUI {
			ui := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
					Networks:    networks,
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			return ui
		}

		createExposer := func(name, uiName, componentName string, selfConfig *runtime.RawExtension, basePath ...string) *uiv1alpha1.ScalityUIComponentExposer {
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					SelfConfiguration:  selfConfig,
				},
			}
			if len(basePath) > 0 {
				exposer.Spec.AppHistoryBasePath = basePath[0]
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			return exposer
		}

		createReconciler := func() *ScalityUIComponentExposerReconciler {
			return &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
		}

		reconcileExposer := func(reconciler *ScalityUIComponentExposerReconciler, exposerName string) (ctrl.Result, error) {
			return reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposerName,
					Namespace: testNamespace,
				},
			})
		}

		typeNamespacedName := types.NamespacedName{
			Name:      exposerName,
			Namespace: testNamespace,
		}

		BeforeEach(func() {
			// Create ScalityUI resource with default auth
			_ = createScalityUI(uiName, nil)

			// Create ScalityUIComponent resource with PublicPath
			_ = createComponent(componentName, "/"+componentName)
		})

		AfterEach(func() {
			// Clean up resources
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			_ = k8sClient.Get(ctx, typeNamespacedName, exposer)
			_ = k8sClient.Delete(ctx, exposer)

			// Trigger reconcile to run finalizer logic if the exposer is still present
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, exposer)
				if err != nil {
					return client.IgnoreNotFound(err) == nil // true when not found
				}
				// Still exists, run another reconcile to process deletion
				_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				return false
			}, time.Second*5, time.Millisecond*200).Should(BeTrue())

			ui := &uiv1alpha1.ScalityUI{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: uiName}, ui)
			_ = k8sClient.Delete(ctx, ui)

			component := &uiv1alpha1.ScalityUIComponent{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, component)
			_ = k8sClient.Delete(ctx, component)
		})

		It("should successfully reconcile the resource with no auth configuration", func() {
			By("Creating the custom resource for the Kind ScalityUIComponentExposer")
			_ = createExposer(exposerName, uiName, componentName, nil, "/test-app")

			By("Reconciling the created resource")
			controllerReconciler := createReconciler()
			_, err := reconcileExposer(controllerReconciler, exposerName)
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
			Expect(configMap.Data).To(HaveKey(exposerName))

			var runtimeConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data[exposerName]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(runtimeConfig.Kind).To(Equal("MicroAppRuntimeConfiguration"))
			Expect(runtimeConfig.APIVersion).To(Equal("ui.scality.com/v1alpha1"))
			Expect(runtimeConfig.Metadata.Kind).To(Equal(""))
			Expect(runtimeConfig.Metadata.Name).To(Equal(componentName))
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

		It("should handle complete auth configuration from ScalityUI", func() {
			By("Creating ScalityUI with complete auth configuration")
			providerLogout := true
			uiWithAuth := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ui-with-auth",
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
					Auth: &uiv1alpha1.AuthConfig{
						Kind:           "OIDC",
						ProviderURL:    "https://auth.example.com",
						RedirectURL:    "/callback",
						ClientID:       "test-client",
						ResponseType:   "code",
						Scopes:         "openid profile email",
						ProviderLogout: &providerLogout,
					},
				},
			}
			Expect(k8sClient.Create(ctx, uiWithAuth)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, uiWithAuth)
			}()

			By("Creating the custom resource referencing UI with auth")
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
					ScalityUI:          "test-ui-with-auth",
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
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
			err = json.Unmarshal([]byte(configMap.Data[exposerName]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(runtimeConfig.Metadata.Name).To(Equal(componentName))

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

		It("should handle missing ScalityUI dependency", func() {
			By("Creating the custom resource with non-existent ScalityUI")
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
					ScalityUIComponent: componentName, // Use existing component
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
			Expect(depCondition.Message).To(ContainSubstring("ScalityUI \"non-existent-ui\" not found"))
		})

		It("should require PublicPath to be set in component status", func() {
			By("Creating a component missing PublicPath")
			componentMissingPath := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-missing-public-path",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/component:latest",
					MountPath: "/usr/share/nginx/html/.well-known",
				},
				// Status.PublicPath intentionally missing
			}
			Expect(k8sClient.Create(ctx, componentMissingPath)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, componentMissingPath) }()

			By("Creating exposer referencing component without PublicPath")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-missing-public-path",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName, // Use existing UI
					ScalityUIComponent: "test-component-missing-public-path",
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposer) }()

			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-missing-public-path",
					Namespace: testNamespace,
				},
			})

			By("Expecting reconciliation to succeed with ConfigMap but fail on Ingress due to missing PublicPath")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not have PublicPath set in status"))

			By("Verifying ConfigMap was still created")
			configMap := &corev1.ConfigMap{}
			configMapName := "test-component-missing-public-path-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			By("Checking status conditions")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-missing-public-path",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			// ConfigMap should be ready
			configMapCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "ConfigMapReady")
			Expect(configMapCondition).NotTo(BeNil())
			Expect(configMapCondition.Status).To(Equal(metav1.ConditionTrue))

			// But reconciliation should fail due to Ingress error
			ingressCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "IngressReady")
			Expect(ingressCondition).NotTo(BeNil())
			Expect(ingressCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(ingressCondition.Message).To(ContainSubstring("does not have PublicPath set in status"))
		})

		It("should handle missing ScalityUIComponent dependency", func() {
			By("Creating the custom resource with non-existent ScalityUIComponent")
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
					ScalityUI:          uiName, // Use existing UI
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

		It("should handle exposer spec update with invalid ScalityUI reference", func() {
			By("Creating a working exposer first")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-invalid-ui",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName, // Start with valid UI
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposer) }()

			By("Reconciling the working exposer successfully")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-invalid-ui",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating exposer to reference invalid ScalityUI")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-invalid-ui",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			updatedExposer.Spec.ScalityUI = "non-existent-ui"
			Expect(k8sClient.Update(ctx, updatedExposer)).To(Succeed())

			By("Reconciling after invalid update")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-invalid-ui",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Minute))

			By("Verifying dependency error condition")
			finalExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-invalid-ui",
				Namespace: testNamespace,
			}, finalExposer)).To(Succeed())

			depCondition := meta.FindStatusCondition(finalExposer.Status.Conditions, "DependenciesReady")
			Expect(depCondition).NotTo(BeNil())
			Expect(depCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCondition.Reason).To(Equal("DependencyMissing"))
			Expect(depCondition.Message).To(ContainSubstring("ScalityUI \"non-existent-ui\" not found"))
		})

		It("should handle exposer spec update with invalid ScalityUIComponent reference", func() {
			By("Creating a working exposer first")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-invalid-component",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName, // Start with valid component
					AppHistoryBasePath: "/test-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposer) }()

			By("Reconciling the working exposer successfully")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-invalid-component",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating exposer to reference invalid ScalityUIComponent")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-invalid-component",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			updatedExposer.Spec.ScalityUIComponent = "non-existent-component"
			Expect(k8sClient.Update(ctx, updatedExposer)).To(Succeed())

			By("Reconciling after invalid update")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-invalid-component",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Minute))

			By("Verifying dependency error condition")
			finalExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-invalid-component",
				Namespace: testNamespace,
			}, finalExposer)).To(Succeed())

			depCondition := meta.FindStatusCondition(finalExposer.Status.Conditions, "DependenciesReady")
			Expect(depCondition).NotTo(BeNil())
			Expect(depCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCondition.Reason).To(Equal("DependencyMissing"))
			Expect(depCondition.Message).To(ContainSubstring("ScalityUIComponent \"non-existent-component\" not found"))
		})

		It("should update ConfigMap when exposer spec changes", func() {
			By("Creating exposer with initial configuration")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-spec-update",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: componentName,
					AppHistoryBasePath: "/initial-app",
					SelfConfiguration: &runtime.RawExtension{
						Raw: []byte(`{"feature": "initial", "version": 1}`),
					},
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposer) }()

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling initial exposer")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-spec-update",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying initial ConfigMap content")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			var initialConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data["test-exposer-spec-update"]), &initialConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialConfig.Spec.AppHistoryBasePath).To(Equal("/initial-app"))
			Expect(initialConfig.Spec.SelfConfiguration["feature"]).To(Equal("initial"))
			Expect(initialConfig.Spec.SelfConfiguration["version"]).To(Equal(float64(1)))

			By("Updating exposer spec with new configuration")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-spec-update",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			updatedExposer.Spec.AppHistoryBasePath = "/updated-app"
			updatedExposer.Spec.SelfConfiguration = &runtime.RawExtension{
				Raw: []byte(`{"feature": "updated", "version": 2, "newField": "added"}`),
			}
			Expect(k8sClient.Update(ctx, updatedExposer)).To(Succeed())

			By("Reconciling updated exposer")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-spec-update",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap was updated")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
				if err != nil {
					return false
				}

				var updatedConfig MicroAppRuntimeConfiguration
				err = json.Unmarshal([]byte(configMap.Data["test-exposer-spec-update"]), &updatedConfig)
				if err != nil {
					return false
				}

				return updatedConfig.Spec.AppHistoryBasePath == "/updated-app" &&
					updatedConfig.Spec.SelfConfiguration["feature"] == "updated" &&
					updatedConfig.Spec.SelfConfiguration["version"] == float64(2) &&
					updatedConfig.Spec.SelfConfiguration["newField"] == "added"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			By("Verifying final ConfigMap content")
			var finalConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data["test-exposer-spec-update"]), &finalConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(finalConfig.Spec.AppHistoryBasePath).To(Equal("/updated-app"))
			Expect(finalConfig.Spec.SelfConfiguration["feature"]).To(Equal("updated"))
			Expect(finalConfig.Spec.SelfConfiguration["version"]).To(Equal(float64(2)))
			Expect(finalConfig.Spec.SelfConfiguration["newField"]).To(Equal("added"))
		})

		It("should update ConfigMap when ScalityUI configuration changes", func() {
			By("Creating ScalityUI with initial auth configuration")
			providerLogout := true
			uiWithAuth := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ui-auth-update",
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
					Auth: &uiv1alpha1.AuthConfig{
						Kind:           "OIDC",
						ProviderURL:    "https://initial-auth.example.com",
						RedirectURL:    "/initial-callback",
						ClientID:       "initial-client",
						ResponseType:   "code",
						Scopes:         "openid profile email",
						ProviderLogout: &providerLogout,
					},
				},
			}
			Expect(k8sClient.Create(ctx, uiWithAuth)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, uiWithAuth) }()

			By("Creating the exposer")
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
					ScalityUI:          "test-ui-auth-update",
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

			By("Updating the ScalityUI auth configuration")
			updatedUI := &uiv1alpha1.ScalityUI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-ui-auth-update"}, updatedUI)).To(Succeed())

			providerLogout = false
			updatedUI.Spec.Auth = &uiv1alpha1.AuthConfig{
				Kind:           "OIDC",
				ProviderURL:    "https://updated-auth.example.com",
				RedirectURL:    "/updated-callback",
				ClientID:       "updated-client",
				ResponseType:   "code",
				Scopes:         "openid profile",
				ProviderLogout: &providerLogout,
			}
			Expect(k8sClient.Update(ctx, updatedUI)).To(Succeed())

			By("Reconciling the exposer after UI auth update")
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
				err = json.Unmarshal([]byte(configMap.Data[exposerName]), &runtimeConfig)
				if err != nil {
					return false
				}

				authConfig := runtimeConfig.Spec.Auth
				return authConfig["providerUrl"] == "https://updated-auth.example.com" &&
					authConfig["clientId"] == "updated-client"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())
		})

		It("should set correct finalizer on ConfigMap", func() {
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

			By("Verifying ConfigMap finalizer")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			finalizerName := configMapFinalizerPrefix + exposerName
			Expect(configMap.Finalizers).To(ContainElement(finalizerName))

			By("Verifying exposer has its own finalizer")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: exposerName, Namespace: testNamespace}, updatedExposer)).To(Succeed())
			Expect(updatedExposer.Finalizers).To(ContainElement(exposerFinalizer))
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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uiName}, ui)).To(Succeed())

			_ = controllerReconciler.findExposersForScalityUI(ctx, ui)

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

		It("should update deployment with ConfigMap mount and trigger rolling update", func() {
			By("Creating component with deployment")
			component := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-mount",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					MountPath: "/usr/share/nginx/html/.well-known",
				},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())
			// Update status separately since it's a subresource
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-component-mount", Namespace: testNamespace}, component); err != nil {
					return err
				}
				component.Status.Kind = "test-kind"
				component.Status.PublicPath = "/test-component-mount"
				return k8sClient.Status().Update(ctx, component)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, component) }()

			// Create deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-mount",
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "test:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, deployment) }()

			By("Creating exposer")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-mount",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,
					ScalityUIComponent: "test-component-mount",
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposer) }()

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Updating deployment with ConfigMap mount")
			err := controllerReconciler.updateComponentDeployment(ctx, component, exposer, "test-hash", log.FromContext(ctx))
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment was updated")
			updatedDeployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-component-mount",
				Namespace: testNamespace,
			}, updatedDeployment)).To(Succeed())

			// Check volume
			Expect(updatedDeployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := updatedDeployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("config-volume-test-component-mount"))
			Expect(volume.ConfigMap.Name).To(Equal("test-component-mount-runtime-app-configuration"))

			// Check volume mount
			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			mount := updatedDeployment.Spec.Template.Spec.Containers[0].VolumeMounts[0]
			Expect(mount.Name).To(Equal("config-volume-test-component-mount"))
			Expect(mount.MountPath).To(Equal("/usr/share/nginx/html/.well-known/" + configsSubdirectory))
			Expect(mount.SubPath).To(Equal(""))
			Expect(mount.ReadOnly).To(BeTrue())

			// Check annotation
			Expect(updatedDeployment.Spec.Template.Annotations).To(HaveKey("ui.scality.com/config-hash"))
			Expect(updatedDeployment.Spec.Template.Annotations["ui.scality.com/config-hash"]).To(ContainSubstring("test-hash"))
		})

		It("should ensure volume and volume mount correctly", func() {
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Testing ensureConfigMapVolume")
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{},
						},
					},
				},
			}

			// First call should add volume
			changed := controllerReconciler.ensureConfigMapVolume(deployment, "test-volume", "test-configmap")
			Expect(changed).To(BeTrue())
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Volumes[0].Name).To(Equal("test-volume"))
			Expect(deployment.Spec.Template.Spec.Volumes[0].ConfigMap.Name).To(Equal("test-configmap"))

			// Second call with same params should not change
			changed = controllerReconciler.ensureConfigMapVolume(deployment, "test-volume", "test-configmap")
			Expect(changed).To(BeFalse())

			// Call with different configmap name should update
			changed = controllerReconciler.ensureConfigMapVolume(deployment, "test-volume", "different-configmap")
			Expect(changed).To(BeTrue())
			Expect(deployment.Spec.Template.Spec.Volumes[0].ConfigMap.Name).To(Equal("different-configmap"))

			By("Testing ensureConfigMapVolumeMount")
			container := &corev1.Container{
				VolumeMounts: []corev1.VolumeMount{},
			}

			// First call should add mount
			testMountPath := "/usr/share/nginx/html/.well-known/" + configsSubdirectory
			changed = controllerReconciler.ensureConfigMapVolumeMount(container, "test-volume", testMountPath)
			Expect(changed).To(BeTrue())
			Expect(container.VolumeMounts).To(HaveLen(1))
			Expect(container.VolumeMounts[0].Name).To(Equal("test-volume"))
			Expect(container.VolumeMounts[0].MountPath).To(Equal(testMountPath))
			Expect(container.VolumeMounts[0].SubPath).To(Equal(""))
			Expect(container.VolumeMounts[0].ReadOnly).To(BeTrue())

			// Second call with same params should not change
			changed = controllerReconciler.ensureConfigMapVolumeMount(container, "test-volume", testMountPath)
			Expect(changed).To(BeFalse())

			// Modify mount and verify it gets corrected
			container.VolumeMounts[0].ReadOnly = false
			container.VolumeMounts[0].MountPath = "/wrong/path"
			changed = controllerReconciler.ensureConfigMapVolumeMount(container, "test-volume", testMountPath)
			Expect(changed).To(BeTrue())
			Expect(container.VolumeMounts[0].ReadOnly).To(BeTrue())
			Expect(container.VolumeMounts[0].MountPath).To(Equal(testMountPath))
		})

		It("should handle deployment not found gracefully", func() {
			By("Creating component without deployment")
			component := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-no-deployment",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					MountPath: "/usr/share/nginx/html/.well-known",
				},
			}
			Expect(k8sClient.Create(ctx, component)).To(Succeed())
			// Update status separately since it's a subresource
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-component-no-deployment", Namespace: testNamespace}, component); err != nil {
					return err
				}
				component.Status.PublicPath = "/test-component-no-deployment"
				return k8sClient.Status().Update(ctx, component)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, component) }()

			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-no-deployment",
					Namespace: testNamespace,
				},
			}

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Attempting to update non-existent deployment")
			err := controllerReconciler.updateComponentDeployment(ctx, component, exposer, "test-hash", log.FromContext(ctx))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle self configuration parsing correctly", func() {
			By("Creating exposer with complex self configuration")
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
					SelfConfiguration: &runtime.RawExtension{
						Raw: []byte(`{
							"apiUrl": "https://api.example.com",
							"features": {
								"enableFeatureA": true,
								"enableFeatureB": false
							},
							"limits": {
								"maxItems": 100,
								"timeout": 30
							}
						}`),
					},
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying self configuration in ConfigMap")
			configMap := &corev1.ConfigMap{}
			configMapName := componentName + "-runtime-app-configuration"
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMapName,
					Namespace: testNamespace,
				}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			var runtimeConfig MicroAppRuntimeConfiguration
			err = json.Unmarshal([]byte(configMap.Data[exposerName]), &runtimeConfig)
			Expect(err).NotTo(HaveOccurred())

			selfConfig := runtimeConfig.Spec.SelfConfiguration
			Expect(selfConfig["apiUrl"]).To(Equal("https://api.example.com"))

			features, ok := selfConfig["features"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(features["enableFeatureA"]).To(Equal(true))
			Expect(features["enableFeatureB"]).To(Equal(false))

			limits, ok := selfConfig["limits"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(limits["maxItems"]).To(Equal(float64(100))) // JSON numbers are float64
			Expect(limits["timeout"]).To(Equal(float64(30)))
		})

		It("should create Ingress when networks are configured in ScalityUI", func() {
			By("Creating ScalityUI with networks configuration")
			uiWithNetworks := &uiv1alpha1.ScalityUI{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ui-with-networks",
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
					Networks: &uiv1alpha1.UINetworks{
						Host:             "ui.example.com",
						IngressClassName: "nginx",
						IngressAnnotations: map[string]string{
							"nginx.ingress.kubernetes.io/ssl-redirect": "true",
						},
						TLS: []networkingv1.IngressTLS{
							{
								Hosts:      []string{"ui.example.com"},
								SecretName: "ui-tls-secret",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, uiWithNetworks)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, uiWithNetworks) }()

			By("Creating component with public path")
			componentWithPath := &uiv1alpha1.ScalityUIComponent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-with-path",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image:     "scality/component:latest",
					MountPath: "/usr/share/nginx/html/.well-known",
				},
				Status: uiv1alpha1.ScalityUIComponentStatus{
					PublicPath: "/my-app",
				},
			}
			Expect(k8sClient.Create(ctx, componentWithPath)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, componentWithPath) }()

			// Update status separately since it's a subresource
			componentWithPath.Status.PublicPath = "/my-app"
			Expect(k8sClient.Status().Update(ctx, componentWithPath)).To(Succeed())

			By("Creating exposer that inherits networks configuration")
			exposerWithIngress := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-with-ingress",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          "test-ui-with-networks",
					ScalityUIComponent: "test-component-with-path",
					AppHistoryBasePath: "/my-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposerWithIngress)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposerWithIngress) }()

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the exposer")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-with-ingress",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Ingress was created")
			ingress := &networkingv1.Ingress{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-exposer-with-ingress",
					Namespace: testNamespace,
				}, ingress)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			By("Verifying Ingress configuration")
			Expect(ingress.Spec.IngressClassName).NotTo(BeNil())
			Expect(*ingress.Spec.IngressClassName).To(Equal("nginx"))

			Expect(ingress.Spec.Rules).To(HaveLen(1))
			rule := ingress.Spec.Rules[0]
			Expect(rule.Host).To(Equal("ui.example.com"))

			Expect(rule.IngressRuleValue.HTTP.Paths).To(HaveLen(1))

			appPath := rule.IngressRuleValue.HTTP.Paths[0]
			Expect(appPath.Path).To(Equal("/my-app/"))
			Expect(*appPath.PathType).To(Equal(networkingv1.PathTypePrefix))

			// Verify TLS configuration
			Expect(ingress.Spec.TLS).To(HaveLen(1))
			Expect(ingress.Spec.TLS[0].Hosts).To(ContainElement("ui.example.com"))
			Expect(ingress.Spec.TLS[0].SecretName).To(Equal("ui-tls-secret"))

			// Verify annotations including configuration snippet
			Expect(ingress.Annotations).To(HaveKey("nginx.ingress.kubernetes.io/ssl-redirect"))
			Expect(ingress.Annotations).To(HaveKey("nginx.ingress.kubernetes.io/configuration-snippet"))
			configSnippet := ingress.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"]
			Expect(configSnippet).To(ContainSubstring("/my-app/\\\\.well-known/runtime-app-configuration"))
			Expect(configSnippet).To(ContainSubstring("/.well-known/configs/test-exposer-with-ingress"))

			By("Checking IngressReady status condition")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-with-ingress",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			ingressCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "IngressReady")
			Expect(ingressCondition).NotTo(BeNil())
			Expect(ingressCondition.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should not create Ingress when networks are not configured", func() {
			By("Creating exposer using default UI and component (which have no networks configuration)")
			exposerNoNetworks := &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-no-networks",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          uiName,        // Reuse existing UI constant (no networks)
					ScalityUIComponent: componentName, // Reuse existing component
				},
			}
			Expect(k8sClient.Create(ctx, exposerNoNetworks)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposerNoNetworks) }()

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the exposer")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-no-networks",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no Ingress was created")
			ingress := &networkingv1.Ingress{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-exposer-no-networks",
					Namespace: testNamespace,
				}, ingress)
				return err != nil
			}, time.Second*2, time.Millisecond*200).Should(BeTrue())

			By("Checking IngressReady status condition")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-no-networks",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			ingressCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "IngressReady")
			Expect(ingressCondition).NotTo(BeNil())
			Expect(ingressCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(ingressCondition.Message).To(ContainSubstring("Ingress not needed"))
		})

		It("should inherit networks configuration from ScalityUI only", func() {
			By("Creating ScalityUI with networks configuration")
			uiWithNetworks := &uiv1alpha1.ScalityUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ui-inherit-networks",
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "Test Product",
					Networks: &uiv1alpha1.UINetworks{
						Host:             "inherit.example.com",
						IngressClassName: "inherit-ingress",
						IngressAnnotations: map[string]string{
							"nginx.ingress.kubernetes.io/ssl-redirect": "true",
							"nginx.ingress.kubernetes.io/rate-limit":   "200",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, uiWithNetworks)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, uiWithNetworks) }()

			By("Creating component for inheritance test")
			componentInherit := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-inherit",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					MountPath: "/usr/share/nginx/html/.well-known",
				},
				Status: uiv1alpha1.ScalityUIComponentStatus{
					PublicPath: "/inherit-app",
				},
			}
			Expect(k8sClient.Create(ctx, componentInherit)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, componentInherit) }()

			// Update status separately since it's a subresource
			componentInherit.Status.PublicPath = "/inherit-app"
			Expect(k8sClient.Status().Update(ctx, componentInherit)).To(Succeed())

			By("Creating exposer that inherits networks configuration from ScalityUI")
			exposerInherit := &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-inherit",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          "test-ui-inherit-networks",
					ScalityUIComponent: "test-component-inherit",
					AppHistoryBasePath: "/inherit-app",
				},
			}
			Expect(k8sClient.Create(ctx, exposerInherit)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposerInherit) }()

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the exposer with inherited configuration")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-inherit",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Ingress was created with inherited configuration")
			ingress := &networkingv1.Ingress{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-exposer-inherit",
					Namespace: testNamespace,
				}, ingress)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			By("Verifying complete networks configuration inheritance from ScalityUI")
			Expect(*ingress.Spec.IngressClassName).To(Equal("inherit-ingress"))
			Expect(ingress.Spec.Rules[0].Host).To(Equal("inherit.example.com"))
			Expect(ingress.Spec.Rules[0].IngressRuleValue.HTTP.Paths[0].Path).To(Equal("/inherit-app/"))

			Expect(ingress.Annotations["nginx.ingress.kubernetes.io/ssl-redirect"]).To(Equal("true"))
			Expect(ingress.Annotations["nginx.ingress.kubernetes.io/rate-limit"]).To(Equal("200"))
			Expect(ingress.Annotations).To(HaveKey("nginx.ingress.kubernetes.io/configuration-snippet"))
		})

		It("should fail when component PublicPath is missing", func() {
			By("Creating ScalityUI with networks configuration")
			uiForPath := &uiv1alpha1.ScalityUI{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ui-missing-path"},
				Spec: uiv1alpha1.ScalityUISpec{
					Networks: &uiv1alpha1.UINetworks{
						Host:             "missing-path.example.com",
						IngressClassName: "nginx",
					},
				},
			}
			Expect(k8sClient.Create(ctx, uiForPath)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, uiForPath) }()

			By("Creating component without PublicPath")
			componentNoPath := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-component-missing-path",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					MountPath: "/usr/share/nginx/html/.well-known",
				},
				// No PublicPath in Status
			}
			Expect(k8sClient.Create(ctx, componentNoPath)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, componentNoPath) }()

			exposerMissingPath := &uiv1alpha1.ScalityUIComponentExposer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-missing-path",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          "test-ui-missing-path",
					ScalityUIComponent: "test-component-missing-path",
				},
			}
			Expect(k8sClient.Create(ctx, exposerMissingPath)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposerMissingPath) }()

			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling exposer with missing PublicPath")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-missing-path",
					Namespace: testNamespace,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not have PublicPath set in status"))

			By("Verifying IngressReady condition shows failure")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-missing-path",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			ingressCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "IngressReady")
			Expect(ingressCondition).NotTo(BeNil())
			Expect(ingressCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(ingressCondition.Reason).To(Equal("ReconcileFailed"))
			Expect(ingressCondition.Message).To(ContainSubstring("does not have PublicPath set in status"))

			By("Verifying no Ingress was created")
			ingress := &networkingv1.Ingress{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-exposer-missing-path",
					Namespace: testNamespace,
				}, ingress)
				return err != nil
			}, time.Second*2, time.Millisecond*200).Should(BeTrue())
		})
	})
})
