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
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

var _ = Describe("ScalityUI Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName             = "test-ui"
			resourceNamespace        = "default"
			productName              = "Test Product"
			imageName                = "nginx:latest"
			baseMountPath            = "/usr/share/nginx/html/shell" // Updated to match controller
			configMapDeployedAppsExt = "-deployed-apps"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		scalityui := &uiv1alpha1.ScalityUI{}

		BeforeEach(func() {
			By("Attempting to fetch existing ScalityUI resource")
			err := k8sClient.Get(ctx, typeNamespacedName, scalityui)
			if err != nil {
				if errors.IsNotFound(err) {
					By("ScalityUI resource not found, creating a new one")
					resource := &uiv1alpha1.ScalityUI{
						ObjectMeta: metav1.ObjectMeta{
							Name:      resourceName,
							Namespace: resourceNamespace,
						},
						Spec: uiv1alpha1.ScalityUISpec{
							Image:       imageName,
							ProductName: productName,
						},
					}
					Expect(k8sClient.Create(ctx, resource)).To(Succeed())

					// Crucially, fetch the resource again to populate scalityui with generated metadata like UID
					By("Fetching the newly created ScalityUI resource to get its UID")
					Eventually(func() error {
						return k8sClient.Get(ctx, typeNamespacedName, scalityui)
					}, time.Second*10, time.Millisecond*250).Should(Succeed(), "Failed to fetch newly created ScalityUI resource after creation")
				} else {
					// Other error fetching, fail the test
					Expect(err).NotTo(HaveOccurred(), "Failed to get ScalityUI resource in BeforeEach (non-NotFound error)")
				}
			} else {
				By("ScalityUI resource found, UID: " + string(scalityui.UID))
			}
		})

		AfterEach(func() {
			resource := &uiv1alpha1.ScalityUI{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance ScalityUI")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Also delete the ConfigMaps and Deployment if they exist
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}

			configMapDeployedApps := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + configMapDeployedAppsExt, Namespace: resourceNamespace}, configMapDeployedApps)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMapDeployedApps)).To(Succeed())
			}

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource, creating all sub-resources", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    GinkgoLogr,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify main ConfigMap (config.json)
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, createdConfigMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(createdConfigMap.Data).To(HaveKey("config.json"))
			var configData map[string]interface{}
			Expect(json.Unmarshal([]byte(createdConfigMap.Data["config.json"]), &configData)).To(Succeed())

			Expect(configData["productName"]).To(Equal(productName))
			Expect(configData["discoveryUrl"]).To(Equal("/shell/deployed-ui-apps.json"))

			navbarData, ok := configData["navbar"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "navbar should be a map")
			Expect(navbarData["main"]).To(BeEmpty(), "default navbar.main should be empty array")
			Expect(navbarData["subLogin"]).To(BeEmpty(), "default navbar.subLogin should be empty array")

			themesData, ok := configData["themes"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "themes should be a map")
			Expect(themesData["light"]).To(HaveKeyWithValue("name", "artescaLight"))
			Expect(themesData["dark"]).To(HaveKeyWithValue("name", "darkRebrand"))

			// Verify deployed-apps ConfigMap
			deployedAppsCm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + configMapDeployedAppsExt, Namespace: resourceNamespace}, deployedAppsCm)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())
			Expect(deployedAppsCm.Data).To(HaveKeyWithValue("deployed-ui-apps.json", "[]"))

			// Verify Deployment
			createdDeployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, createdDeployment)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(createdDeployment.Spec.Replicas).To(Equal(&[]int32{1}[0]))
			Expect(createdDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(createdDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal(imageName))
			Expect(createdDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2)) // Expecting 2 volume mounts

			Expect(createdDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElements(
				corev1.VolumeMount{
					Name:      resourceName + "-config-volume",
					MountPath: filepath.Join(baseMountPath, "config.json"),
					SubPath:   "config.json",
				},
				corev1.VolumeMount{
					Name:      resourceName + "-deployed-apps-volume",
					MountPath: filepath.Join(baseMountPath, "deployed-ui-apps.json"),
					SubPath:   "deployed-ui-apps.json",
				},
			))

			Expect(createdDeployment.Spec.Template.Spec.Volumes).To(HaveLen(2)) // Expecting 2 volumes

			defaultMode := int32(0644) // 420 in decimal

			Expect(createdDeployment.Spec.Template.Spec.Volumes).To(ContainElements(
				corev1.Volume{
					Name: resourceName + "-config-volume",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: resourceName},
							DefaultMode:          &defaultMode,
						},
					},
				},
				corev1.Volume{
					Name: resourceName + "-deployed-apps-volume",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: resourceName + configMapDeployedAppsExt},
							DefaultMode:          &defaultMode,
						},
					},
				},
			))

			// Verify Deployment Strategy
			Expect(createdDeployment.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(createdDeployment.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(createdDeployment.Spec.Strategy.RollingUpdate.MaxUnavailable.Type).To(Equal(intstr.Int))
			Expect(createdDeployment.Spec.Strategy.RollingUpdate.MaxUnavailable.IntVal).To(Equal(int32(0)))
			Expect(createdDeployment.Spec.Strategy.RollingUpdate.MaxSurge.Type).To(Equal(intstr.Int))
			Expect(createdDeployment.Spec.Strategy.RollingUpdate.MaxSurge.IntVal).To(Equal(int32(1)))

			// Verify Pod Template Annotations
			Expect(createdDeployment.Spec.Template.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(createdDeployment.Spec.Template.ObjectMeta.Annotations).To(HaveKey("checksum/config"))

			// Verify OwnerReferences on ConfigMaps
			Expect(createdConfigMap.OwnerReferences).NotTo(BeEmpty())
			Expect(createdConfigMap.OwnerReferences[0].UID).To(Equal(scalityui.UID))
			Expect(deployedAppsCm.OwnerReferences).NotTo(BeEmpty())
			Expect(deployedAppsCm.OwnerReferences[0].UID).To(Equal(scalityui.UID))

			// Verify OwnerReferences on Deployment
			Expect(createdDeployment.OwnerReferences).NotTo(BeEmpty())
			Expect(createdDeployment.OwnerReferences[0].UID).To(Equal(scalityui.UID))

		})

		It("should update ConfigMap with custom navbar and themes when the resource is updated", func() {
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    GinkgoLogr,
			}

			By("Initial reconcile")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the ScalityUI resource")
			currentScalityUI := &uiv1alpha1.ScalityUI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, currentScalityUI)).To(Succeed())

			By("Updating the ScalityUI resource with custom navbar and themes")
			currentScalityUI.Spec.ProductName = "Updated Product Name"
			currentScalityUI.Spec.Navbar = uiv1alpha1.Navbar{
				Main:     []uiv1alpha1.NavbarItem{{Kind: "test", View: "view1"}},
				SubLogin: []uiv1alpha1.NavbarItem{{Kind: "subtest", View: "view2"}},
			}
			currentScalityUI.Spec.Themes = uiv1alpha1.Themes{
				Light: uiv1alpha1.Theme{Type: "custom", Name: "customLight", LogoPath: "/light.png"},
				Dark:  uiv1alpha1.Theme{Type: "custom", Name: "customDark", LogoPath: "/dark.png"},
			}
			Expect(k8sClient.Update(ctx, currentScalityUI)).To(Succeed())

			By("Reconciling again after update")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the updated ConfigMap for config.json")
			updatedConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, updatedConfigMap)
				if err != nil {
					return false
				}
				var configData map[string]interface{}
				err = json.Unmarshal([]byte(updatedConfigMap.Data["config.json"]), &configData)
				return err == nil && configData["productName"] == "Updated Product Name"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			var configData map[string]interface{}
			Expect(json.Unmarshal([]byte(updatedConfigMap.Data["config.json"]), &configData)).To(Succeed())

			Expect(configData["productName"]).To(Equal("Updated Product Name"))
			navbarData, ok := configData["navbar"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(navbarData["main"]).To(HaveLen(1))
			Expect(navbarData["main"].([]interface{})[0].(map[string]interface{})["kind"]).To(Equal("test"))
			Expect(navbarData["subLogin"]).To(HaveLen(1))
			Expect(navbarData["subLogin"].([]interface{})[0].(map[string]interface{})["kind"]).To(Equal("subtest"))

			themesData, ok := configData["themes"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(themesData["light"]).To(HaveKeyWithValue("name", "customLight"))
			Expect(themesData["dark"]).To(HaveKeyWithValue("name", "customDark"))
		})

		// Removed redundant tests for ConfigMap and Deployment creation as they are covered in the main reconcile test.
		// The tests for updates still provide value for checking update logic specifically.

		It("should update Deployment image when the resource Spec.Image is updated", func() {
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    GinkgoLogr,
			}

			By("Initial reconcile")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the ScalityUI resource")
			currentScalityUI := &uiv1alpha1.ScalityUI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, currentScalityUI)).To(Succeed())

			By("Updating the ScalityUI resource image")
			newImage := "nginx:1.21"
			currentScalityUI.Spec.Image = newImage
			Expect(k8sClient.Update(ctx, currentScalityUI)).To(Succeed())

			By("Reconciling again after image update")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the updated Deployment image")
			updatedDeployment := &appsv1.Deployment{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, updatedDeployment)
				if err != nil {
					return ""
				}
				if len(updatedDeployment.Spec.Template.Spec.Containers) > 0 {
					return updatedDeployment.Spec.Template.Spec.Containers[0].Image
				}
				return ""
			}, time.Second*10, time.Millisecond*250).Should(Equal(newImage))

			// Check volume mounts again, should remain 2
			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2))
		})

		Describe("createConfigJSON function", func() {
			It("should generate default config when navbar and themes are not specified", func() {
				testUI := &uiv1alpha1.ScalityUI{
					Spec: uiv1alpha1.ScalityUISpec{
						ProductName: "Default Product",
						// No Navbar, Themes, Favicon, Logo, CanChange* specified
					},
				}

				configBytes, err := createConfigJSON(testUI)
				Expect(err).NotTo(HaveOccurred())

				var configData map[string]interface{}
				Expect(json.Unmarshal(configBytes, &configData)).To(Succeed())

				Expect(configData).To(HaveKeyWithValue("productName", "Default Product"))
				Expect(configData).To(HaveKeyWithValue("discoveryUrl", "/shell/deployed-ui-apps.json"))
				Expect(configData).To(HaveKeyWithValue("canChangeTheme", false))
				Expect(configData).To(HaveKeyWithValue("canChangeInstanceName", false))
				Expect(configData).To(HaveKeyWithValue("canChangeLanguage", false))
				Expect(configData).To(HaveKeyWithValue("favicon", ""))
				Expect(configData).To(HaveKeyWithValue("logo", ""))

				navbarData, ok := configData["navbar"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(navbarData["main"]).To(BeEmpty())
				Expect(navbarData["subLogin"]).To(BeEmpty())

				themesData, ok := configData["themes"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(themesData["light"]).To(SatisfyAll(
					HaveKeyWithValue("type", "core-ui"),
					HaveKeyWithValue("name", "artescaLight"),
					HaveKeyWithValue("logoPath", ""),
				))
				Expect(themesData["dark"]).To(SatisfyAll(
					HaveKeyWithValue("type", "core-ui"),
					HaveKeyWithValue("name", "darkRebrand"),
					HaveKeyWithValue("logoPath", ""),
				))
			})

			It("should generate config with custom values when specified", func() {
				customNavbar := uiv1alpha1.Navbar{
					Main:     []uiv1alpha1.NavbarItem{{Kind: "mainLink", View: "mainView"}},
					SubLogin: []uiv1alpha1.NavbarItem{{Kind: "subLink", View: "subView"}},
				}
				customThemes := uiv1alpha1.Themes{
					Light: uiv1alpha1.Theme{Type: "custom", Name: "myLight", LogoPath: "/light.svg"},
					Dark:  uiv1alpha1.Theme{Type: "custom", Name: "myDark", LogoPath: "/dark.svg"},
				}
				testUI := &uiv1alpha1.ScalityUI{
					Spec: uiv1alpha1.ScalityUISpec{
						ProductName:           "Custom Product",
						Navbar:                customNavbar,
						Themes:                customThemes,
						CanChangeTheme:        true,
						CanChangeInstanceName: true,
						CanChangeLanguage:     true,
						Favicon:               "/myfavicon.ico",
						Logo:                  "/mylogo.png",
					},
				}

				configBytes, err := createConfigJSON(testUI)
				Expect(err).NotTo(HaveOccurred())

				var configData map[string]interface{}
				Expect(json.Unmarshal(configBytes, &configData)).To(Succeed())

				Expect(configData).To(HaveKeyWithValue("productName", "Custom Product"))
				Expect(configData).To(HaveKeyWithValue("discoveryUrl", "/shell/deployed-ui-apps.json"))
				Expect(configData).To(HaveKeyWithValue("canChangeTheme", true))
				Expect(configData).To(HaveKeyWithValue("canChangeInstanceName", true))
				Expect(configData).To(HaveKeyWithValue("canChangeLanguage", true))
				Expect(configData).To(HaveKeyWithValue("favicon", "/myfavicon.ico"))
				Expect(configData).To(HaveKeyWithValue("logo", "/mylogo.png"))

				navbarData, ok := configData["navbar"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(navbarData["main"]).To(HaveLen(1))
				mainItems, _ := navbarData["main"].([]interface{})
				Expect(mainItems[0].(map[string]interface{})["kind"]).To(Equal("mainLink"))
				Expect(navbarData["subLogin"]).To(HaveLen(1))
				subLoginItems, _ := navbarData["subLogin"].([]interface{})
				Expect(subLoginItems[0].(map[string]interface{})["kind"]).To(Equal("subLink"))

				themesData, ok := configData["themes"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(themesData["light"]).To(HaveKeyWithValue("name", "myLight"))
				Expect(themesData["dark"]).To(HaveKeyWithValue("name", "myDark"))
			})
		})
	})
})
