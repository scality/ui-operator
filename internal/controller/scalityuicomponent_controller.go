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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

func int32Ptr(i int32) *int32 {
	return &i
}

func (r *ScalityUIComponentReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appsv1.Deployment, scalityUIComponent *uiv1alpha1.ScalityUIComponent) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": scalityUIComponent.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": scalityUIComponent.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: scalityUIComponent.Name, Image: scalityUIComponent.Spec.Image},
					},
				},
			},
		}

		return nil
	})
}

type ScalityUIComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ScalityUIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	scalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
	if err := r.Get(ctx, req.NamespacedName, scalityUIComponent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityUIComponent.Name,
			Namespace: scalityUIComponent.Namespace,
		},
	}

	deploymentResult, err := r.createOrUpdateDeployment(ctx, deploy, scalityUIComponent)
	if err != nil {
		logger.Error(err, "Failed to create or update deployment for scalityUIComponent")
		return ctrl.Result{}, err
	}
	logger.Info("Deployment reconciled", "result", deploymentResult)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponent{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
