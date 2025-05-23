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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

var _ = Describe("ScalityUIComponentExposer Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName  = "test-exposer"
			testNamespace = "default"
			uiName        = "test-ui"
			componentName = "test-component"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		scalityuicomponentexposer := &uiv1alpha1.ScalityUIComponentExposer{}

		BeforeEach(func() {
			// First create the ScalityUI
			ui := &uiv1alpha1.ScalityUI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uiName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUISpec{
					Image:       "scality/ui:latest",
					ProductName: "test-product",
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: uiName, Namespace: testNamespace}, ui)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			}

			// Then create the ScalityUIComponent
			component := &uiv1alpha1.ScalityUIComponent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      componentName,
					Namespace: testNamespace,
				},
				Spec: uiv1alpha1.ScalityUIComponentSpec{
					Image: "scality/ui-component:latest",
				},
			}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, component)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, component)).To(Succeed())
			}

			// Create a deployment for the component (normally created by the component controller)
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      componentName,
					Namespace: testNamespace,
				},
				Spec: appsv1.DeploymentSpec{
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
									Name:  componentName,
									Image: "scality/ui-component:latest",
								},
							},
						},
					},
				},
			}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, &appsv1.Deployment{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			}

			deployedAppsConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uiName + "-deployed-ui-apps",
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"deployed-ui-apps.json": "[]",
				},
			}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: uiName + "-deployed-ui-apps", Namespace: testNamespace}, &corev1.ConfigMap{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, deployedAppsConfigMap)).To(Succeed())
			}

			// Finally create the ScalityUIComponentExposer
			err = k8sClient.Get(ctx, typeNamespacedName, scalityuicomponentexposer)
			if err != nil && errors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUIComponentExposer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
						ScalityUI:          uiName,
						ScalityUIComponent: componentName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// Cleanup all resources
			resource := &uiv1alpha1.ScalityUIComponentExposer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Cleanup ingress
			ingress := &networkingv1.Ingress{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-ingress", Namespace: testNamespace}, ingress)
			if err == nil {
				Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())
			}

			// Cleanup ConfigMap
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: testNamespace}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}

			// Cleanup Component
			component := &uiv1alpha1.ScalityUIComponent{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, component)
			if err == nil {
				Expect(k8sClient.Delete(ctx, component)).To(Succeed())
			}

			// Cleanup Component Deployment
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}

			// Cleanup UI
			ui := &uiv1alpha1.ScalityUI{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: uiName, Namespace: testNamespace}, ui)
			if err == nil {
				Expect(k8sClient.Delete(ctx, ui)).To(Succeed())
			}

			// Cleanup deployed-ui-apps ConfigMap
			deployedAppsConfigMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: uiName + "-deployed-ui-apps", Namespace: testNamespace}, deployedAppsConfigMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployedAppsConfigMap)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if Ingress was created")
			ingress := &networkingv1.Ingress{}
			ingressName := fmt.Sprintf("%s-ingress", resourceName)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: testNamespace}, ingress)
			}).Should(Succeed())

			By("Checking if ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-runtime-app-configuration", componentName), Namespace: testNamespace}, configMap)
			}).Should(Succeed())
			Expect(configMap.Data).To(HaveKey("runtime-app-configuration"))

			By("Checking if Deployment was updated with volume mount")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: testNamespace}, deployment)
			}).Should(Succeed())

			// Check for volume
			volumeName := "config-volume-" + componentName
			foundVolume := false
			for _, vol := range deployment.Spec.Template.Spec.Volumes {
				if vol.Name == volumeName {
					foundVolume = true
					Expect(vol.ConfigMap).NotTo(BeNil())
					Expect(vol.ConfigMap.Name).To(Equal(fmt.Sprintf("%s-runtime-app-configuration", componentName)))
					break
				}
			}
			Expect(foundVolume).To(BeTrue(), "Expected to find config volume in deployment")

			// Check for volume mount
			foundMount := false
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, mount := range container.VolumeMounts {
					if mount.Name == volumeName {
						foundMount = true
						Expect(mount.MountPath).To(Equal("/usr/share/nginx/html/.well-known/runtime-app-configuration"))
						Expect(mount.SubPath).To(Equal("runtime-app-configuration"))
						break
					}
				}
			}
			Expect(foundMount).To(BeTrue(), "Expected to find volume mount in deployment container")

			// Check for hash annotation
			Expect(deployment.Spec.Template.Annotations).To(HaveKey("ui.scality.com/config-hash"))
		})
	})
})
