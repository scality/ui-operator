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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

var _ = Describe("ScalityUIComponent Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-component"
			resourceNamespace = "default"
			imageName         = "nginx:latest"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		scalityuicomponent := &uiv1alpha1.ScalityUIComponent{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ScalityUIComponent")
			err := k8sClient.Get(ctx, typeNamespacedName, scalityuicomponent)
			if err != nil && errors.IsNotFound(err) {
				resource := &uiv1alpha1.ScalityUIComponent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: uiv1alpha1.ScalityUIComponentSpec{
						Image: imageName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &uiv1alpha1.ScalityUIComponent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance ScalityUIComponent")
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
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully create a Deployment", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
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
		})

		It("should update Deployment when the resource is updated", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalityUIComponentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, scalityuicomponent)
			Expect(err).NotTo(HaveOccurred())

			newImage := "nginx:1.21"
			scalityuicomponent.Spec.Image = newImage
			Expect(k8sClient.Update(ctx, scalityuicomponent)).To(Succeed())

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
		})
	})
})
