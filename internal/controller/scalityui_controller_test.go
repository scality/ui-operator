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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

var _ = Describe("ScalityUI Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-ui"
			resourceNamespace = "default"
			productName       = "Test Product"
			imageName         = "nginx:latest"
			mountPath         = "/usr/share/nginx/html/custom"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		scalityui := &uiv1alpha1.ScalityUI{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ScalityUI")
			err := k8sClient.Get(ctx, typeNamespacedName, scalityui)
			if err != nil && errors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUI{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: uiv1alpha1.ScalityUISpec{
						Image:       imageName,
						ProductName: productName,
						MountPath:   mountPath,
						Navbar: uiv1alpha1.Navbar{
							Main:     []uiv1alpha1.NavbarItem{},
							SubLogin: []uiv1alpha1.NavbarItem{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &uiv1alpha1.ScalityUI{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance ScalityUI")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Also delete the ConfigMap and Deployment if they exist
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}

			// Also delete the Service if it exists
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})

		It("should successfully create a ConfigMap", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap was created
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(configMap.Data).To(HaveKey("config.json"))
			Expect(configMap.Data["config.json"]).To(ContainSubstring(productName))
		})

		It("should successfully create a Deployment", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment was created
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, deployment)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(deployment.Spec.Replicas).To(Equal(&[]int32{1}[0]))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(imageName))
			Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal(mountPath + "/config.json"))

			// Verify Deployment Strategy
			Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(deployment.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.Type).To(Equal(intstr.Int))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.IntVal).To(Equal(int32(0)))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.Type).To(Equal(intstr.Int))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.IntVal).To(Equal(int32(1)))

			// Verify Pod Template Annotations
			Expect(deployment.Spec.Template.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(deployment.Spec.Template.ObjectMeta.Annotations).To(HaveKey("checksum/config"))
		})

		It("should successfully create a Service", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Service was created
			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, service)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(service.Spec.Selector).To(Equal(map[string]string{"app": resourceName}))
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).To(Equal("http"))
			Expect(service.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(service.Spec.Ports[0].TargetPort).To(Equal(intstr.FromInt(80)))
			// Default service type is ClusterIP, can be asserted if needed
			// Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		})

		It("should update ConfigMap when the resource is updated", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, scalityui)
			Expect(err).NotTo(HaveOccurred())

			scalityui.Spec.ProductName = "Updated Product"
			Expect(k8sClient.Update(ctx, scalityui)).To(Succeed())

			// Reconcile the resource again
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ConfigMap was updated
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, configMap)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(configMap.Data["config.json"]).To(ContainSubstring("Updated Product"))
		})

		It("should update Deployment when the resource is updated", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, scalityui)
			Expect(err).NotTo(HaveOccurred())

			newImage := "nginx:1.21"
			scalityui.Spec.Image = newImage
			Expect(k8sClient.Update(ctx, scalityui)).To(Succeed())

			// Reconcile the resource again
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment was updated
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, deployment)
			}, time.Second*10, time.Millisecond*250).Should(Succeed())

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(newImage))
			Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal(mountPath + "/config.json"))

			// Verify Deployment Strategy after update
			Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(deployment.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.Type).To(Equal(intstr.Int))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.IntVal).To(Equal(int32(0)))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.Type).To(Equal(intstr.Int))
			Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.IntVal).To(Equal(int32(1)))

			// Verify Pod Template Annotations after update
			Expect(deployment.Spec.Template.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(deployment.Spec.Template.ObjectMeta.Annotations).To(HaveKey("checksum/config"))
		})

		It("should test createConfigJSON function directly", func() {
			testUI := &uiv1alpha1.ScalityUI{
				Spec: uiv1alpha1.ScalityUISpec{
					ProductName: "Test Product Direct",
				},
			}

			configJSON, err := createConfigJSON(testUI)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(configJSON)).To(ContainSubstring("Test Product Direct"))
		})

	})
})
