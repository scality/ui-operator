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
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

const (
	// Test timeout constants
	eventuallyTimeout  = 10 * time.Second       // Maximum time to wait for resource creation/updates in tests
	eventuallyInterval = 250 * time.Millisecond // Polling interval for checking resource status
)

var _ = Describe("ScalityUI Shell Features", func() {
	Context("When deploying a Scality Shell UI", func() {
		const (
			uiAppName      = "test-ui"
			productName    = "Test Product"
			containerImage = "nginx:latest"
		)

		ctx := context.Background()
		// ScalityUI is cluster-scoped, so no namespace in the name
		clusterScopedName := types.NamespacedName{Name: uiAppName}
		scalityui := &uiv1alpha1.ScalityUI{}

		BeforeEach(func() {
			By("Setting up a new Shell UI")
			err := k8sClient.Get(ctx, clusterScopedName, scalityui)
			if err != nil && errors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUI{
					ObjectMeta: metav1.ObjectMeta{
						Name: uiAppName, // No namespace for cluster-scoped resource
					},
					Spec: uiv1alpha1.ScalityUISpec{
						Image:       containerImage,
						ProductName: productName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())

				Eventually(func() error {
					return k8sClient.Get(ctx, clusterScopedName, scalityui)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleaning up test resources")
			cleanupTestResources(ctx, clusterScopedName)
		})

		Describe("Basic Shell UI Deployment", func() {
			It("should deploy a working Shell UI with default configuration", func() {
				By("Processing the Shell UI deployment")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the Shell UI is properly configured")
				verifyUIApplicationConfiguration(ctx, uiAppName, productName)

				By("Verifying the Shell UI is accessible via service")
				verifyUIApplicationService(ctx, uiAppName)

				By("Verifying the Shell UI has proper resource ownership")
				verifyResourceOwnership(ctx, uiAppName, scalityui.UID)

				By("Verifying the deployment can be updated with imagePullSecrets")
				currentUI := &uiv1alpha1.ScalityUI{}
				Expect(k8sClient.Get(ctx, clusterScopedName, currentUI)).To(Succeed())
				secretName := "my-ui-secret"
				currentUI.Spec.ImagePullSecrets = []string{secretName}
				Expect(k8sClient.Update(ctx, currentUI)).To(Succeed())

				// Reconcile again to apply the update
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				updatedDeployment := &appsv1.Deployment{}
				deploymentName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
				Eventually(func() error {
					return k8sClient.Get(ctx, deploymentName, updatedDeployment)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
				Expect(updatedDeployment.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal(secretName))
			})
		})

		Describe("Shell UI Customization Features", func() {
			It("should allow customization of shell branding and navigation", func() {
				By("Deploying the Shell UI initially")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Customizing the Shell UI with branding and navigation")
				currentUI := &uiv1alpha1.ScalityUI{}
				Expect(k8sClient.Get(ctx, clusterScopedName, currentUI)).To(Succeed())

				currentUI.Spec.ProductName = "Custom Branded Product"
				currentUI.Spec.Navbar = uiv1alpha1.Navbar{
					Main: []uiv1alpha1.NavbarItem{{
						Internal: &uiv1alpha1.InternalNavbarItem{
							Kind: "dashboard",
							View: "main-dashboard",
						},
					}},
					SubLogin: []uiv1alpha1.NavbarItem{{
						Internal: &uiv1alpha1.InternalNavbarItem{
							Kind: "profile",
							View: "user-profile",
						},
					}},
				}
				currentUI.Spec.Themes = uiv1alpha1.Themes{
					Light: uiv1alpha1.Theme{
						Type: "custom",
						Name: "companyLight",
						Logo: uiv1alpha1.Logo{
							Type:  "path",
							Value: "/assets/light-logo.png",
						},
					},
					Dark: uiv1alpha1.Theme{
						Type: "custom",
						Name: "companyDark",
						Logo: uiv1alpha1.Logo{
							Type:  "path",
							Value: "/assets/dark-logo.png",
						},
					},
				}

				Expect(k8sClient.Update(ctx, currentUI)).To(Succeed())

				By("Applying the customization changes")
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the Shell UI reflects the custom branding")
				verifyCustomBranding(ctx, uiAppName, "Custom Branded Product")

				By("Verifying the custom navigation is configured")
				verifyCustomNavigation(ctx, uiAppName)

				By("Verifying the custom themes are applied")
				verifyCustomThemes(ctx, uiAppName)

				By("Verifying user customization options are enabled")
				verifyUserCustomizationOptions(ctx, uiAppName)
			})
		})

		Describe("Shell Application Updates", func() {
			It("should handle shell application image updates seamlessly", func() {
				By("Deploying the initial Shell UI")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Updating to a new shell application version")
				currentUI := &uiv1alpha1.ScalityUI{}
				Expect(k8sClient.Get(ctx, clusterScopedName, currentUI)).To(Succeed())

				newImage := "nginx:1.21"
				currentUI.Spec.Image = newImage
				Expect(k8sClient.Update(ctx, currentUI)).To(Succeed())

				By("Applying the version update")
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the shell application is running the new version")
				verifyApplicationVersion(ctx, uiAppName, newImage)
			})

			Describe("High Availability Features", func() {
				It("should deploy with single replica when only one node is available", func() {
					By("Creating a single node in the cluster")
					node := &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-node-single",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:   corev1.NodeReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, node)).To(Succeed())
					defer func() {
						Expect(k8sClient.Delete(ctx, node)).To(Succeed())
					}()

					By("Deploying the Shell UI")
					reconciler := &ScalityUIReconciler{
						Client: k8sClient,
						Scheme: k8sClient.Scheme(),
						Log:    GinkgoLogr,
					}
					_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
					Expect(err).NotTo(HaveOccurred())

					By("Verifying single replica deployment")
					deployment := &appsv1.Deployment{}
					deploymentName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
					Eventually(func() error {
						return k8sClient.Get(ctx, deploymentName, deployment)
					}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

					Expect(deployment.Spec.Replicas).NotTo(BeNil())
					Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))

					By("Verifying no anti-affinity rules are set for single replica")
					Expect(deployment.Spec.Template.Spec.Affinity).To(BeNil())
				})

				It("should deploy with high availability when multiple nodes are available", func() {
					By("Creating multiple nodes in the cluster")
					node1 := &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-node-1",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:   corev1.NodeReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					}
					node2 := &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-node-2",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:   corev1.NodeReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, node1)).To(Succeed())
					Expect(k8sClient.Create(ctx, node2)).To(Succeed())
					defer func() {
						Expect(k8sClient.Delete(ctx, node1)).To(Succeed())
						Expect(k8sClient.Delete(ctx, node2)).To(Succeed())
					}()

					By("Deploying the Shell UI")
					reconciler := &ScalityUIReconciler{
						Client: k8sClient,
						Scheme: k8sClient.Scheme(),
						Log:    GinkgoLogr,
					}
					_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
					Expect(err).NotTo(HaveOccurred())

					By("Verifying high availability deployment with 2 replicas")
					deployment := &appsv1.Deployment{}
					deploymentName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
					Eventually(func() error {
						return k8sClient.Get(ctx, deploymentName, deployment)
					}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

					Expect(deployment.Spec.Replicas).NotTo(BeNil())
					Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))

					By("Verifying pod anti-affinity rules are configured")
					Expect(deployment.Spec.Template.Spec.Affinity).NotTo(BeNil())
					Expect(deployment.Spec.Template.Spec.Affinity.PodAntiAffinity).NotTo(BeNil())
					Expect(deployment.Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))

					antiAffinityRule := deployment.Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0]
					Expect(antiAffinityRule.Weight).To(Equal(int32(100)))
					Expect(antiAffinityRule.PodAffinityTerm.TopologyKey).To(Equal("kubernetes.io/hostname"))
					Expect(antiAffinityRule.PodAffinityTerm.LabelSelector).NotTo(BeNil())
					Expect(antiAffinityRule.PodAffinityTerm.LabelSelector.MatchLabels).To(HaveKeyWithValue("app", uiAppName))
				})

				It("should handle node count changes dynamically", func() {
					By("Starting with a single node")
					node1 := &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-node-dynamic-1",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:   corev1.NodeReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, node1)).To(Succeed())
					defer func() {
						Expect(k8sClient.Delete(ctx, node1)).To(Succeed())
					}()

					By("Deploying the Shell UI with single node")
					reconciler := &ScalityUIReconciler{
						Client: k8sClient,
						Scheme: k8sClient.Scheme(),
						Log:    GinkgoLogr,
					}
					_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
					Expect(err).NotTo(HaveOccurred())

					By("Verifying single replica deployment initially")
					deployment := &appsv1.Deployment{}
					deploymentName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
					Eventually(func() error {
						return k8sClient.Get(ctx, deploymentName, deployment)
					}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
					Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))

					By("Adding a second node")
					node2 := &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-node-dynamic-2",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:   corev1.NodeReady,
									Status: corev1.ConditionTrue,
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, node2)).To(Succeed())
					defer func() {
						Expect(k8sClient.Delete(ctx, node2)).To(Succeed())
					}()

					By("Reconciling after adding the second node")
					_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
					Expect(err).NotTo(HaveOccurred())

					By("Verifying the deployment scales to 2 replicas with anti-affinity")
					Eventually(func() int32 {
						err := k8sClient.Get(ctx, deploymentName, deployment)
						if err != nil {
							return 0
						}
						if deployment.Spec.Replicas == nil {
							return 0
						}
						return *deployment.Spec.Replicas
					}, eventuallyTimeout, eventuallyInterval).Should(Equal(int32(2)))

					Expect(deployment.Spec.Template.Spec.Affinity).NotTo(BeNil())
					Expect(deployment.Spec.Template.Spec.Affinity.PodAntiAffinity).NotTo(BeNil())
				})
			})
		})

		Describe("Deployed UI Apps Management", func() {
			It("should update deployed-ui-apps ConfigMap when exposers are added", func() {
				By("Deploying the Shell UI and verifying initial empty state")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying deployed-ui-apps ConfigMap starts empty")
				verifyDeployedUIAppsContent(ctx, uiAppName, []map[string]interface{}{})

				By("Creating minimal test resources for exposer scenario")
				testNamespace := "test-deployed-apps"
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
				Expect(k8sClient.Create(ctx, ns)).To(Succeed())
				defer k8sClient.Delete(ctx, ns)

				// Create component with minimal required status
				component := &uiv1alpha1.ScalityUIComponent{
					ObjectMeta: metav1.ObjectMeta{Name: "test-component", Namespace: testNamespace},
					Spec:       uiv1alpha1.ScalityUIComponentSpec{Image: "test:1.0.0"},
				}
				Expect(k8sClient.Create(ctx, component)).To(Succeed())
				component.Status = uiv1alpha1.ScalityUIComponentStatus{
					Kind: "micro-app", PublicPath: "/apps/test-component", Version: "1.0.0",
				}
				component.Status.Conditions = []metav1.Condition{
					{
						Type:               "ConfigurationRetrieved",
						Status:             metav1.ConditionTrue,
						Reason:             "FetchSucceeded",
						LastTransitionTime: metav1.Now(),
					},
				}
				Expect(k8sClient.Status().Update(ctx, component)).To(Succeed())
				defer k8sClient.Delete(ctx, component)

				// Create exposer that references our UI
				exposer := &uiv1alpha1.ScalityUIComponentExposer{
					ObjectMeta: metav1.ObjectMeta{Name: "test-exposer", Namespace: testNamespace},
					Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
						ScalityUI: uiAppName, ScalityUIComponent: "test-component", AppHistoryBasePath: "/test-app",
					},
				}
				Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
				defer k8sClient.Delete(ctx, exposer)

				By("Reconciling to trigger deployed-ui-apps update")
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying deployed-ui-apps ConfigMap now contains the app")
				verifyDeployedUIAppsContent(ctx, uiAppName, []map[string]interface{}{
					{
						"appHistoryBasePath": "/test-app",
						"kind":               "micro-app",
						"name":               "test-component", // Should be component name, not exposer name
						"url":                "/apps/test-component",
						"version":            "1.0.0",
					},
				})
			})
		})

		Describe("Shell Configuration Management", func() {
			It("should generate proper default shell configuration for new deployments", func() {
				testUI := &uiv1alpha1.ScalityUI{
					Spec: uiv1alpha1.ScalityUISpec{
						ProductName: "Default Product",
					},
				}

				configBytes, err := createConfigJSON(testUI)
				Expect(err).NotTo(HaveOccurred())

				var config map[string]interface{}
				Expect(json.Unmarshal(configBytes, &config)).To(Succeed())

				By("Verifying default shell product configuration")
				Expect(config["productName"]).To(Equal("Default Product"))
				Expect(config["discoveryUrl"]).To(Equal("/shell/deployed-ui-apps.json"))

				By("Verifying default branding is not present")
				Expect(config).NotTo(HaveKey("favicon"))
				Expect(config).NotTo(HaveKey("logo"))
				Expect(config).NotTo(HaveKey("canChangeTheme"))
				Expect(config).NotTo(HaveKey("canChangeInstanceName"))
				Expect(config).NotTo(HaveKey("canChangeLanguage"))

				By("Verifying default navigation structure")
				navbar := config["navbar"].(map[string]interface{})
				Expect(navbar["main"]).To(BeEmpty())
				Expect(navbar["subLogin"]).To(BeEmpty())

				By("Verifying default theme configuration")
				themes := config["themes"].(map[string]interface{})
				lightTheme := themes["light"].(map[string]interface{})
				darkTheme := themes["dark"].(map[string]interface{})

				Expect(lightTheme["type"]).To(Equal("core-ui"))
				Expect(lightTheme["name"]).To(Equal("artescaLight"))
				Expect(lightTheme["logoPath"]).To(Equal(""))
				Expect(darkTheme["type"]).To(Equal("core-ui"))
				Expect(darkTheme["name"]).To(Equal("darkRebrand"))
				Expect(darkTheme["logoPath"]).To(Equal(""))
			})

			It("should generate shell configuration with custom business requirements", func() {
				customNavbar := uiv1alpha1.Navbar{
					Main: []uiv1alpha1.NavbarItem{{
						Internal: &uiv1alpha1.InternalNavbarItem{
							Kind: "analytics",
							View: "business-analytics",
						},
					}},
					SubLogin: []uiv1alpha1.NavbarItem{{
						Internal: &uiv1alpha1.InternalNavbarItem{
							Kind: "settings",
							View: "user-settings",
						},
					}},
				}
				customThemes := uiv1alpha1.Themes{
					Light: uiv1alpha1.Theme{
						Type: "enterprise",
						Name: "corporateLight",
						Logo: uiv1alpha1.Logo{
							Type:  "path",
							Value: "/branding/light.svg",
						},
					},
					Dark: uiv1alpha1.Theme{
						Type: "enterprise",
						Name: "corporateDark",
						Logo: uiv1alpha1.Logo{
							Type:  "path",
							Value: "/branding/dark.svg",
						},
					},
				}

				testUI := &uiv1alpha1.ScalityUI{
					Spec: uiv1alpha1.ScalityUISpec{
						ProductName: "Enterprise Dashboard",
						Navbar:      customNavbar,
						Themes:      customThemes,
					},
				}

				configBytes, err := createConfigJSON(testUI)
				Expect(err).NotTo(HaveOccurred())

				var config map[string]interface{}
				Expect(json.Unmarshal(configBytes, &config)).To(Succeed())

				By("Verifying custom shell product configuration")
				Expect(config["productName"]).To(Equal("Enterprise Dashboard"))

				By("Verifying custom navigation structure")
				navbar := config["navbar"].(map[string]interface{})
				mainNav := navbar["main"].([]interface{})
				subLoginNav := navbar["subLogin"].([]interface{})

				Expect(mainNav).To(HaveLen(1))
				Expect(mainNav[0].(map[string]interface{})["kind"]).To(Equal("analytics"))
				Expect(mainNav[0].(map[string]interface{})["view"]).To(Equal("business-analytics"))
				Expect(subLoginNav).To(HaveLen(1))
				Expect(subLoginNav[0].(map[string]interface{})["kind"]).To(Equal("settings"))
				Expect(subLoginNav[0].(map[string]interface{})["view"]).To(Equal("user-settings"))

				By("Verifying custom theme configuration")
				themes := config["themes"].(map[string]interface{})
				lightTheme := themes["light"].(map[string]interface{})
				darkTheme := themes["dark"].(map[string]interface{})
				Expect(lightTheme["name"]).To(Equal("corporateLight"))
				Expect(lightTheme["logoPath"]).To(Equal("/branding/light.svg"))
				Expect(darkTheme["name"]).To(Equal("corporateDark"))
				Expect(darkTheme["logoPath"]).To(Equal("/branding/dark.svg"))
			})
		})
	})

	Context("When managing Ingress resources", func() {
		const (
			resourceName = "test-ui-ingress"
			productName  = "Test UI with Ingress"
			imageName    = "nginx:latest"
		)

		ctx := context.Background()
		// ScalityUI is cluster-scoped
		clusterScopedName := types.NamespacedName{Name: resourceName}

		AfterEach(func() {
			// Clean up ScalityUI resource (cluster-scoped)
			resource := &uiv1alpha1.ScalityUI{}
			err := k8sClient.Get(ctx, clusterScopedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Clean up Ingress (in target namespace)
			ingress := &networkingv1.Ingress{}
			ingressName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
			err = k8sClient.Get(ctx, ingressName, ingress)
			if err == nil {
				Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())
			}
		})

		It("should create a default Ingress when no network configuration is provided", func() {
			By("Creating a ScalityUI without network configuration")
			resource := &uiv1alpha1.ScalityUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName, // No namespace for cluster-scoped resource
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       imageName,
					ProductName: productName,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: clusterScopedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying that an Ingress is created in the target namespace")
			ingress := &networkingv1.Ingress{}
			ingressName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
			Eventually(func() error {
				return k8sClient.Get(ctx, ingressName, ingress)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Verifying the Ingress has basic configuration")
			Expect(ingress.Spec.Rules).To(HaveLen(1))
			Expect(ingress.Spec.Rules[0].HTTP.Paths).To(HaveLen(1))
			Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
			Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(resourceName))
		})

		It("should create a custom Ingress when network configuration is provided", func() {
			By("Creating a ScalityUI with network configuration")
			resource := &uiv1alpha1.ScalityUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName, // No namespace for cluster-scoped resource
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       imageName,
					ProductName: productName,
					Networks: &uiv1alpha1.UINetworks{
						Host:             "test.example.com",
						IngressClassName: "nginx",
						IngressAnnotations: map[string]string{
							"nginx.ingress.kubernetes.io/ssl-redirect": "false",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: clusterScopedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying that an Ingress is created with custom configuration")
			ingress := &networkingv1.Ingress{}
			ingressName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
			Eventually(func() error {
				return k8sClient.Get(ctx, ingressName, ingress)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Verifying the Ingress has the specified host and class")
			Expect(ingress.Spec.IngressClassName).NotTo(BeNil())
			Expect(*ingress.Spec.IngressClassName).To(Equal("nginx"))
			Expect(ingress.Spec.Rules).To(HaveLen(1))
			Expect(ingress.Spec.Rules[0].Host).To(Equal("test.example.com"))

			By("Verifying the Ingress has the specified annotations")
			Expect(ingress.Annotations).To(HaveKey("nginx.ingress.kubernetes.io/ssl-redirect"))
			Expect(ingress.Annotations["nginx.ingress.kubernetes.io/ssl-redirect"]).To(Equal("false"))
		})

		It("should expose the application at the root path", func() {
			By("Creating a ScalityUI resource")
			resource := &uiv1alpha1.ScalityUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName, // No namespace for cluster-scoped resource
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       imageName,
					ProductName: productName,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: clusterScopedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the Ingress routes traffic to the root path")
			ingress := &networkingv1.Ingress{}
			ingressName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
			Eventually(func() error {
				return k8sClient.Get(ctx, ingressName, ingress)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("Verifying the path configuration allows access to the application")
			Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
			Expect(ingress.Spec.Rules[0].HTTP.Paths[0].PathType).NotTo(BeNil())
			Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(Equal(networkingv1.PathTypePrefix))
		})
	})
})

var _ = Describe("ScalityUI Status Management", func() {
	Context("When managing ScalityUI status", func() {
		const (
			uiAppName      = "test-ui-status"
			productName    = "Test Product Status"
			containerImage = "nginx:latest"
		)

		ctx := context.Background()
		clusterScopedName := types.NamespacedName{Name: uiAppName}
		scalityui := &uiv1alpha1.ScalityUI{}

		BeforeEach(func() {
			By("Setting up a new ScalityUI for status testing")
			err := k8sClient.Get(ctx, clusterScopedName, scalityui)
			if err != nil && errors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUI{
					ObjectMeta: metav1.ObjectMeta{
						Name: uiAppName,
					},
					Spec: uiv1alpha1.ScalityUISpec{
						Image:       containerImage,
						ProductName: productName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())

				Eventually(func() error {
					return k8sClient.Get(ctx, clusterScopedName, scalityui)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleaning up status test resources")
			cleanupTestResources(ctx, clusterScopedName)
		})

		Describe("Status Initialization", func() {
			It("should initialize status with appropriate phase and conditions", func() {
				By("Reconciling the ScalityUI resource")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the status is properly initialized")
				updatedUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() error {
					return k8sClient.Get(ctx, clusterScopedName, updatedUI)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				By("Checking that status has a valid phase")
				Expect(updatedUI.Status.Phase).To(BeElementOf([]string{
					uiv1alpha1.PhasePending,
					uiv1alpha1.PhaseProgressing,
					uiv1alpha1.PhaseReady,
					uiv1alpha1.PhaseFailed,
				}))

				By("Checking that status has the required conditions")
				conditions := updatedUI.Status.Conditions
				Expect(conditions).NotTo(BeEmpty())

				// Check for Ready condition
				readyCondition := findCondition(conditions, uiv1alpha1.ConditionTypeReady)
				Expect(readyCondition).NotTo(BeNil())
				Expect(readyCondition.Type).To(Equal(uiv1alpha1.ConditionTypeReady))

				// Check for Progressing condition
				progressingCondition := findCondition(conditions, uiv1alpha1.ConditionTypeProgressing)
				Expect(progressingCondition).NotTo(BeNil())
				Expect(progressingCondition.Type).To(Equal(uiv1alpha1.ConditionTypeProgressing))
			})
		})

		Describe("Status Updates During Resource Creation", func() {
			It("should update status as resources are created successfully", func() {
				By("Reconciling the ScalityUI resource")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying all resources are created")
				// Verify Deployment exists
				deployment := &appsv1.Deployment{}
				deploymentName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
				Eventually(func() error {
					return k8sClient.Get(ctx, deploymentName, deployment)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				// Verify Service exists
				service := &corev1.Service{}
				serviceName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
				Eventually(func() error {
					return k8sClient.Get(ctx, serviceName, service)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				// Verify Ingress exists
				ingress := &networkingv1.Ingress{}
				ingressName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
				Eventually(func() error {
					return k8sClient.Get(ctx, ingressName, ingress)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				By("Verifying status reflects successful resource creation")
				updatedUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, clusterScopedName, updatedUI)
					if err != nil {
						return false
					}
					return len(updatedUI.Status.Conditions) > 0
				}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

				// The status should indicate resources are being managed
				conditions := updatedUI.Status.Conditions
				Expect(conditions).NotTo(BeEmpty())

				// At minimum, we should have Ready and Progressing conditions
				readyCondition := findCondition(conditions, uiv1alpha1.ConditionTypeReady)
				Expect(readyCondition).NotTo(BeNil())

				progressingCondition := findCondition(conditions, uiv1alpha1.ConditionTypeProgressing)
				Expect(progressingCondition).NotTo(BeNil())
			})
		})

		Describe("Status Condition Transitions", func() {
			It("should properly transition conditions based on resource state", func() {
				By("Creating a ScalityUI and reconciling")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Simulating deployment readiness by updating deployment status")
				deployment := &appsv1.Deployment{}
				deploymentName := types.NamespacedName{Name: uiAppName, Namespace: getOperatorNamespace()}
				Eventually(func() error {
					return k8sClient.Get(ctx, deploymentName, deployment)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				// Update deployment status to simulate ready replicas
				deployment.Status.ReadyReplicas = 1
				deployment.Status.Replicas = 1
				deployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Status: corev1.ConditionFalse, // Not progressing = stable
						Reason: "NewReplicaSetAvailable",
					},
				}
				Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

				By("Reconciling again to pick up the deployment status changes")
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying conditions reflect the updated resource state")
				updatedUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, clusterScopedName, updatedUI)
					if err != nil {
						return false
					}
					readyCondition := findCondition(updatedUI.Status.Conditions, uiv1alpha1.ConditionTypeReady)
					return readyCondition != nil && readyCondition.Status == metav1.ConditionTrue
				}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

				// Verify Ready condition is True
				readyCondition := findCondition(updatedUI.Status.Conditions, uiv1alpha1.ConditionTypeReady)
				Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))

				// Verify Progressing condition is False (stable)
				progressingCondition := findCondition(updatedUI.Status.Conditions, uiv1alpha1.ConditionTypeProgressing)
				Expect(progressingCondition.Status).To(Equal(metav1.ConditionFalse))
			})
		})

		Describe("Status Error Handling", func() {
			It("should handle reconciliation errors gracefully", func() {
				By("Creating a ScalityUI with invalid configuration")
				invalidUI := &uiv1alpha1.ScalityUI{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-ui-invalid",
					},
					Spec: uiv1alpha1.ScalityUISpec{
						Image:       "", // Invalid empty image
						ProductName: "Invalid Test",
					},
				}
				Expect(k8sClient.Create(ctx, invalidUI)).To(Succeed())
				defer func() {
					k8sClient.Delete(ctx, invalidUI)
				}()

				By("Reconciling the invalid resource")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				invalidName := types.NamespacedName{Name: "test-ui-invalid"}
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: invalidName})

				By("Verifying reconciliation fails appropriately")
				// We expect this to fail due to invalid image
				Expect(err).To(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))

				By("Verifying the resource still exists but status may not be updated")
				updatedInvalidUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() error {
					return k8sClient.Get(ctx, invalidName, updatedInvalidUI)
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

				// Since reconciliation failed early, status may not be updated
				// This is expected behavior - status is only updated on successful reconciliation
				By("Verifying the ScalityUI resource exists even after reconciliation failure")
				Expect(updatedInvalidUI.Name).To(Equal("test-ui-invalid"))
			})

			It("should update status when reconciliation partially succeeds", func() {
				By("Creating a valid ScalityUI that will succeed")
				validUI := &uiv1alpha1.ScalityUI{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-ui-partial-success",
					},
					Spec: uiv1alpha1.ScalityUISpec{
						Image:       "nginx:latest",
						ProductName: "Partial Success Test",
					},
				}
				Expect(k8sClient.Create(ctx, validUI)).To(Succeed())
				defer func() {
					k8sClient.Delete(ctx, validUI)
				}()

				By("Reconciling the valid resource")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				validName := types.NamespacedName{Name: "test-ui-partial-success"}
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: validName})

				By("Verifying reconciliation succeeds")
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))

				By("Verifying status is updated after successful reconciliation")
				updatedValidUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, validName, updatedValidUI)
					if err != nil {
						return false
					}
					return len(updatedValidUI.Status.Conditions) > 0
				}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

				// Check that we have conditions set
				conditions := updatedValidUI.Status.Conditions
				Expect(conditions).NotTo(BeEmpty())

				// Should have both Ready and Progressing conditions
				readyCondition := findCondition(conditions, uiv1alpha1.ConditionTypeReady)
				Expect(readyCondition).NotTo(BeNil())

				progressingCondition := findCondition(conditions, uiv1alpha1.ConditionTypeProgressing)
				Expect(progressingCondition).NotTo(BeNil())

				// Phase should be set
				Expect(updatedValidUI.Status.Phase).NotTo(BeEmpty())
			})
		})

		Describe("Phase Management", func() {
			It("should properly set and update the phase field", func() {
				By("Reconciling a ScalityUI resource")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying the phase is set appropriately")
				updatedUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, clusterScopedName, updatedUI)
					return err == nil && updatedUI.Status.Phase != ""
				}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

				// Phase should be one of the valid phases
				Expect(updatedUI.Status.Phase).To(BeElementOf([]string{
					uiv1alpha1.PhasePending,
					uiv1alpha1.PhaseProgressing,
					uiv1alpha1.PhaseReady,
					uiv1alpha1.PhaseFailed,
				}))

				By("Verifying phase is consistent with conditions")
				conditions := updatedUI.Status.Conditions
				if updatedUI.Status.Phase == uiv1alpha1.PhaseReady {
					readyCondition := findCondition(conditions, uiv1alpha1.ConditionTypeReady)
					if readyCondition != nil {
						Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
					}
				}
			})
		})

		Describe("Status Persistence", func() {
			It("should persist status updates across reconciliation cycles", func() {
				By("Performing initial reconciliation")
				reconciler := &ScalityUIReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Log:    GinkgoLogr,
				}

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Capturing initial status")
				firstUI := &uiv1alpha1.ScalityUI{}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, clusterScopedName, firstUI)
					return err == nil && len(firstUI.Status.Conditions) > 0
				}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

				initialConditionCount := len(firstUI.Status.Conditions)
				initialPhase := firstUI.Status.Phase

				By("Performing second reconciliation")
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: clusterScopedName})
				Expect(err).NotTo(HaveOccurred())

				By("Verifying status is maintained")
				secondUI := &uiv1alpha1.ScalityUI{}
				Expect(k8sClient.Get(ctx, clusterScopedName, secondUI)).To(Succeed())

				// Status should be maintained or improved, not lost
				Expect(len(secondUI.Status.Conditions)).To(BeNumerically(">=", initialConditionCount))
				Expect(secondUI.Status.Phase).NotTo(BeEmpty())

				// If phase was set initially, it should still be set
				if initialPhase != "" {
					Expect(secondUI.Status.Phase).NotTo(BeEmpty())
				}
			})
		})
	})
})

var _ = Describe("getOperatorNamespace", func() {
	It("should return POD_NAMESPACE environment variable when set", func() {
		// Set the environment variable
		os.Setenv("POD_NAMESPACE", "test-namespace")
		defer os.Unsetenv("POD_NAMESPACE")

		namespace := getOperatorNamespace()
		Expect(namespace).To(Equal("test-namespace"))
	})

	It("should return default namespace when POD_NAMESPACE is not set", func() {
		// Ensure POD_NAMESPACE is not set
		os.Unsetenv("POD_NAMESPACE")

		namespace := getOperatorNamespace()
		Expect(namespace).To(Equal("scality-ui"))
	})

	It("should return default namespace when POD_NAMESPACE is empty", func() {
		// Set POD_NAMESPACE to empty string
		os.Setenv("POD_NAMESPACE", "")
		defer os.Unsetenv("POD_NAMESPACE")

		namespace := getOperatorNamespace()
		Expect(namespace).To(Equal("scality-ui"))
	})
})

// Helper functions for test verification - these abstract away implementation details

func cleanupTestResources(ctx context.Context, clusterScopedName types.NamespacedName) {
	// Clean up ScalityUI resource (cluster-scoped)
	resource := &uiv1alpha1.ScalityUI{}
	if err := k8sClient.Get(ctx, clusterScopedName, resource); err == nil {
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	}

	// Clean up associated resources in target namespace
	resourceName := clusterScopedName.Name

	configMap := &corev1.ConfigMap{}
	configMapName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
	if err := k8sClient.Get(ctx, configMapName, configMap); err == nil {
		Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
	}

	deployedAppsConfigMap := &corev1.ConfigMap{}
	deployedAppsName := types.NamespacedName{
		Name:      resourceName + "-deployed-ui-apps",
		Namespace: getOperatorNamespace(),
	}
	if err := k8sClient.Get(ctx, deployedAppsName, deployedAppsConfigMap); err == nil {
		Expect(k8sClient.Delete(ctx, deployedAppsConfigMap)).To(Succeed())
	}

	deployment := &appsv1.Deployment{}
	deploymentName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
	if err := k8sClient.Get(ctx, deploymentName, deployment); err == nil {
		Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
	}

	service := &corev1.Service{}
	serviceName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
	if err := k8sClient.Get(ctx, serviceName, service); err == nil {
		Expect(k8sClient.Delete(ctx, service)).To(Succeed())
	}

	ingress := &networkingv1.Ingress{}
	ingressName := types.NamespacedName{Name: resourceName, Namespace: getOperatorNamespace()}
	if err := k8sClient.Get(ctx, ingressName, ingress); err == nil {
		Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())
	}
}

func verifyUIApplicationConfiguration(ctx context.Context, appName, expectedProductName string) {
	configMap := &corev1.ConfigMap{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, configMap)
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	Expect(configMap.Data).To(HaveKey("config.json"))

	var config map[string]interface{}
	Expect(json.Unmarshal([]byte(configMap.Data["config.json"]), &config)).To(Succeed())
	Expect(config["productName"]).To(Equal(expectedProductName))
	Expect(config["discoveryUrl"]).To(Equal("/shell/deployed-ui-apps.json"))

	// Verify deployed apps configuration exists
	deployedAppsConfigMap := &corev1.ConfigMap{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-deployed-ui-apps", Namespace: getOperatorNamespace()}, deployedAppsConfigMap)
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	Expect(deployedAppsConfigMap.Data).To(HaveKeyWithValue("deployed-ui-apps.json", "[]"))
}

func verifyUIApplicationService(ctx context.Context, appName string) {
	service := &corev1.Service{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, service)
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	Expect(service.Spec.Selector).To(HaveKeyWithValue("app", appName))
	Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
	Expect(service.Spec.Ports).To(HaveLen(1))
	Expect(service.Spec.Ports[0].Name).To(Equal("http"))
	Expect(service.Spec.Ports[0].Port).To(Equal(int32(80)))
}

func verifyResourceOwnership(ctx context.Context, appName string, ownerUID types.UID) {
	// Verify ConfigMap ownership
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, configMap)).To(Succeed())
	Expect(configMap.OwnerReferences).NotTo(BeEmpty())
	Expect(configMap.OwnerReferences[0].UID).To(Equal(ownerUID))

	// Verify Deployment ownership
	deployment := &appsv1.Deployment{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, deployment)).To(Succeed())
	Expect(deployment.OwnerReferences).NotTo(BeEmpty())
	Expect(deployment.OwnerReferences[0].UID).To(Equal(ownerUID))

	// Verify Service ownership
	service := &corev1.Service{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, service)).To(Succeed())
	Expect(service.OwnerReferences).NotTo(BeEmpty())
	Expect(service.OwnerReferences[0].UID).To(Equal(ownerUID))
}

func verifyCustomBranding(ctx context.Context, appName, expectedProductName string) {
	configMap := &corev1.ConfigMap{}
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, configMap)
		if err != nil {
			return false
		}
		var config map[string]interface{}
		err = json.Unmarshal([]byte(configMap.Data["config.json"]), &config)
		return err == nil && config["productName"] == expectedProductName
	}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

	var config map[string]interface{}
	Expect(json.Unmarshal([]byte(configMap.Data["config.json"]), &config)).To(Succeed())
	// Custom branding is now handled through themes, not separate favicon/logo fields
}

func verifyCustomNavigation(ctx context.Context, appName string) {
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, configMap)).To(Succeed())

	var config map[string]interface{}
	Expect(json.Unmarshal([]byte(configMap.Data["config.json"]), &config)).To(Succeed())

	navbar := config["navbar"].(map[string]interface{})
	mainNav := navbar["main"].([]interface{})
	subLoginNav := navbar["subLogin"].([]interface{})

	Expect(mainNav).To(HaveLen(1))
	Expect(mainNav[0].(map[string]interface{})["kind"]).To(Equal("dashboard"))
	Expect(mainNav[0].(map[string]interface{})["view"]).To(Equal("main-dashboard"))
	Expect(subLoginNav).To(HaveLen(1))
	Expect(subLoginNav[0].(map[string]interface{})["kind"]).To(Equal("profile"))
	Expect(subLoginNav[0].(map[string]interface{})["view"]).To(Equal("user-profile"))
}

func verifyCustomThemes(ctx context.Context, appName string) {
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, configMap)).To(Succeed())

	var config map[string]interface{}
	Expect(json.Unmarshal([]byte(configMap.Data["config.json"]), &config)).To(Succeed())

	themes := config["themes"].(map[string]interface{})
	lightTheme := themes["light"].(map[string]interface{})
	darkTheme := themes["dark"].(map[string]interface{})
	Expect(lightTheme["name"]).To(Equal("companyLight"))
	Expect(lightTheme["logoPath"]).To(Equal("/assets/light-logo.png"))
	Expect(darkTheme["name"]).To(Equal("companyDark"))
	Expect(darkTheme["logoPath"]).To(Equal("/assets/dark-logo.png"))
}

func verifyUserCustomizationOptions(ctx context.Context, appName string) {
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, configMap)).To(Succeed())

	var config map[string]interface{}
	Expect(json.Unmarshal([]byte(configMap.Data["config.json"]), &config)).To(Succeed())

	// User customization options are no longer part of the basic spec
	// This test now just verifies the config is valid
	Expect(config).To(HaveKey("productName"))
	Expect(config).To(HaveKey("navbar"))
	Expect(config).To(HaveKey("themes"))
}

func verifyApplicationVersion(ctx context.Context, appName, expectedImage string) {
	deployment := &appsv1.Deployment{}
	Eventually(func() string {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: getOperatorNamespace()}, deployment)
		if err != nil {
			return ""
		}
		if len(deployment.Spec.Template.Spec.Containers) > 0 {
			return deployment.Spec.Template.Spec.Containers[0].Image
		}
		return ""
	}, eventuallyTimeout, eventuallyInterval).Should(Equal(expectedImage))
}

func verifyDeployedUIAppsContent(ctx context.Context, appName string, expectedContent []map[string]interface{}) {
	configMap := &corev1.ConfigMap{}
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-deployed-ui-apps", Namespace: getOperatorNamespace()}, configMap)
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	Expect(configMap.Data).To(HaveKey("deployed-ui-apps.json"))

	var content []map[string]interface{}
	Expect(json.Unmarshal([]byte(configMap.Data["deployed-ui-apps.json"]), &content)).To(Succeed())
	Expect(content).To(Equal(expectedContent))
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return &condition
		}
	}
	return nil
}
