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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ctrl "sigs.k8s.io/controller-runtime"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

var _ = Describe("ScalityUIComponentExposer Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			exposerName   = "test-exposer"
			uiName        = "test-ui"
			componentName = "test-component"
			namespace     = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      exposerName,
			Namespace: namespace,
		}

		// Create mock objects
		scalityui := &uiv1alpha1.ScalityUI{}
		scalityuicomponent := &uiv1alpha1.ScalityUIComponent{}
		scalityuicomponentexposer := &uiv1alpha1.ScalityUIComponentExposer{}

		BeforeEach(func() {
			// Create ScalityUI first
			By("creating ScalityUI resource")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: uiName, Namespace: namespace}, scalityui)
			if err != nil && errors.IsNotFound(err) {
				ui := &uiv1alpha1.ScalityUI{
					ObjectMeta: metav1.ObjectMeta{
						Name:      uiName,
						Namespace: namespace,
					},
					Spec: uiv1alpha1.ScalityUISpec{
						Image: "scality/ui:latest",
					},
				}
				Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			}

			// Create ScalityUIComponent with status set
			By("creating ScalityUIComponent resource with status")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, scalityuicomponent)
			if err != nil && errors.IsNotFound(err) {
				component := &uiv1alpha1.ScalityUIComponent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      componentName,
						Namespace: namespace,
					},
					Spec: uiv1alpha1.ScalityUIComponentSpec{
						Image: "scality/ui-component:latest",
					},
				}
				Expect(k8sClient.Create(ctx, component)).To(Succeed())

				// Update status directly
				component.Status.Kind = "testComponent"
				component.Status.PublicPath = "/test-component"
				component.Status.Version = "1.0.0"
				Expect(k8sClient.Status().Update(ctx, component)).To(Succeed())
			}

			// Wait for status to be updated
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, scalityuicomponent)
				if err != nil {
					return false
				}
				return scalityuicomponent.Status.Kind != "" &&
					scalityuicomponent.Status.PublicPath != "" &&
					scalityuicomponent.Status.Version != ""
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			// Now create the ScalityUIComponentExposer
			By("creating the ScalityUIComponentExposer resource")
			err = k8sClient.Get(ctx, typeNamespacedName, scalityuicomponentexposer)
			if err != nil && errors.IsNotFound(err) {
				exposer := &uiv1alpha1.ScalityUIComponentExposer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      exposerName,
						Namespace: namespace,
					},
					Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
						ScalityUI:          uiName,
						ScalityUIComponent: componentName,
					},
				}
				Expect(k8sClient.Create(ctx, exposer)).To(Succeed())
			}

			// Create a deployment for the component
			By("creating a deployment for the ScalityUIComponent")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      componentName,
					Namespace: namespace,
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
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, &appsv1.Deployment{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			}
		})

		AfterEach(func() {
			// Clean up resources in reverse order of creation
			By("Cleaning up resources")

			// Delete ScalityUIComponentExposer
			exposer := &uiv1alpha1.ScalityUIComponentExposer{}
			err := k8sClient.Get(ctx, typeNamespacedName, exposer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, exposer)).To(Succeed())
			}

			// Delete Deployment
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}

			// Delete ScalityUIComponent
			component := &uiv1alpha1.ScalityUIComponent{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, component)
			if err == nil {
				Expect(k8sClient.Delete(ctx, component)).To(Succeed())
			}

			// Delete ScalityUI
			ui := &uiv1alpha1.ScalityUI{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: uiName, Namespace: namespace}, ui)
			if err == nil {
				Expect(k8sClient.Delete(ctx, ui)).To(Succeed())
			}

			// Delete any ConfigMaps created
			configMap := &corev1.ConfigMap{}
			configMapName := fmt.Sprintf("%s-runtime-config", exposerName)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create ConfigMap and update status", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: cfg,
			}

			// Run reconciliation
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Check ConfigMap created
			configMap := &corev1.ConfigMap{}
			configMapName := fmt.Sprintf("%s-runtime-config", exposerName)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: namespace}, configMap)
			Expect(err).NotTo(HaveOccurred())

			// Check ConfigMap data
			Expect(configMap.Data).To(HaveKey("runtime-app-configuration.json"))
			Expect(configMap.Data["runtime-app-configuration.json"]).To(ContainSubstring("testComponent"))
			Expect(configMap.Data["runtime-app-configuration.json"]).To(ContainSubstring("/test-component"))
			Expect(configMap.Data["runtime-app-configuration.json"]).To(ContainSubstring("1.0.0"))

			// Verify status updated on exposer
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedExposer)
				if err != nil {
					return false
				}
				return updatedExposer.Status.Kind == "testComponent" &&
					updatedExposer.Status.PublicPath == "/test-component" &&
					updatedExposer.Status.Version == "1.0.0"
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			// Check conditions
			Expect(updatedExposer.Status.Conditions).NotTo(BeEmpty())
			hasConfigMapCreatedCondition := false
			for _, condition := range updatedExposer.Status.Conditions {
				if condition.Type == "ConfigMapCreated" && condition.Status == metav1.ConditionTrue {
					hasConfigMapCreatedCondition = true
					break
				}
			}
			Expect(hasConfigMapCreatedCondition).To(BeTrue())
		})

		It("should handle missing ScalityUIComponent", func() {
			// Delete the component
			component := &uiv1alpha1.ScalityUIComponent{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, component)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, component)).To(Succeed())

			// Wait for component to be deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: componentName, Namespace: namespace}, component)
				return errors.IsNotFound(err)
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())

			// Run reconciliation
			controllerReconciler := &ScalityUIComponentExposerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: cfg,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Minute))

			// Check condition
			updatedExposer := &uiv1alpha1.ScalityUIComponentExposer{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedExposer)
				if err != nil {
					return false
				}

				for _, condition := range updatedExposer.Status.Conditions {
					if condition.Type == "ComponentAvailable" && condition.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, time.Second*10, time.Millisecond*250).Should(BeTrue())
		})
	})
})
