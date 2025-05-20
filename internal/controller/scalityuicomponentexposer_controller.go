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

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// ScalityUIComponentExposerReconciler reconciles a ScalityUIComponentExposer object
type ScalityUIComponentExposerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalityUIComponentExposerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ScalityUIComponentExposer instance
	exposer := &uiv1alpha1.ScalityUIComponentExposer{}
	if err := r.Get(ctx, req.NamespacedName, exposer); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get ScalityUIComponentExposer")
		return ctrl.Result{}, err
	}

	// Fetch the ScalityUIComponent
	component := &uiv1alpha1.ScalityUIComponent{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      exposer.Spec.ScalityUIComponent,
		Namespace: exposer.Namespace,
	}, component); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUIComponent not found, requeueing", "component", exposer.Spec.ScalityUIComponent)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponent")
		return ctrl.Result{}, err
	}

	// Fetch the ScalityUI
	ui := &uiv1alpha1.ScalityUI{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      exposer.Spec.ScalityUI,
		Namespace: exposer.Namespace,
	}, ui); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUI not found, requeueing", "ui", exposer.Spec.ScalityUI)
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to get ScalityUI")
		return ctrl.Result{}, err
	}

	// Create or update the Ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ingress", exposer.Name),
			Namespace: exposer.Namespace,
		},
	}

	ingressResult, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		// Set controller reference
		if err := ctrl.SetControllerReference(exposer, ingress, r.Scheme); err != nil {
			return err
		}

		if ingress.Labels == nil {
			ingress.Labels = make(map[string]string)
		}
		ingress.Labels["app.kubernetes.io/name"] = exposer.Name
		ingress.Labels["app.kubernetes.io/part-of"] = ui.Spec.ProductName
		ingress.Labels["app.kubernetes.io/component"] = component.Name

		path := fmt.Sprintf("/%s", component.Name)

		// Define ingress spec
		pathType := networkingv1.PathTypePrefix
		ingress.Spec = networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     path,
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: component.Name,
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update Ingress")
		return ctrl.Result{}, err
	}

	logger.Info("Ingress reconciled", "result", ingressResult)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
