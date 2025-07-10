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

package scalityuicomponentexposer

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"github.com/scality/ui-operator/internal/utils"
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

		createExposer := func(name, uiName, componentName string, auth *uiv1alpha1.AuthConfig, selfConfig *runtime.RawExtension, basePath ...string) *uiv1alpha1.ScalityUIComponentExposer {
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
					Auth:               auth,
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
			return NewScalityUIComponentExposerReconcilerForTest(k8sClient, k8sClient.Scheme())
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
			controllerReconciler := NewScalityUIComponentExposerReconcilerForTest(k8sClient, k8sClient.Scheme())
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
			_ = createExposer(exposerName, uiName, componentName, nil, nil, "/test-app")

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
			controllerReconciler := NewScalityUIComponentExposerReconcilerForTest(k8sClient, k8sClient.Scheme())

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

		It("should handle resource not found gracefully", func() {
			By("Reconciling a non-existent resource")
			controllerReconciler := NewScalityUIComponentExposerReconcilerForTest(k8sClient, k8sClient.Scheme())

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

		It("should validate auth configuration correctly", func() {
			By("Testing incomplete auth config")
			incompleteAuth := &uiv1alpha1.AuthConfig{
				ProviderURL: "https://auth.example.com",
				ClientID:    "test-client",
				// Missing: Kind, RedirectURL, ResponseType, Scopes
			}

			err := utils.ValidateAuthConfig(incompleteAuth)
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

			err = utils.ValidateAuthConfig(completeAuth)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle missing dependencies gracefully", func() {
			By("Creating exposer with non-existent ScalityUI")
			exposer := &uiv1alpha1.ScalityUIComponentExposer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "ui.scality.com/v1alpha1",
					Kind:       "ScalityUIComponentExposer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exposer-missing-ui",
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
					ScalityUI:          "non-existent-ui",
					ScalityUIComponent: componentName,
				},
			}
			Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			defer func() { _ = k8sClient.Delete(ctx, exposer) }()

			By("Reconciling the created resource")
			controllerReconciler := NewScalityUIComponentExposerReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-exposer-missing-ui",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking status conditions for missing dependency")
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-exposer-missing-ui",
				Namespace: testNamespace,
			}, updatedExposer)).To(Succeed())

			depCondition := meta.FindStatusCondition(updatedExposer.Status.Conditions, "DependenciesReady")
			Expect(depCondition).NotTo(BeNil())
			Expect(depCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCondition.Reason).To(Equal("DependencyMissing"))
			Expect(depCondition.Message).To(ContainSubstring("ScalityUI \"non-existent-ui\" not found"))
		})
	})
})
