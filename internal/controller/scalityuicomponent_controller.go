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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// ScalityUIComponentReconciler reconciles a ScalityUIComponent object
type ScalityUIComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ScalityUIComponent object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *ScalityUIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	// Service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityUIComponent.Name,
			Namespace: scalityUIComponent.Namespace,
		},
	}

	serviceResult, err := ctrl.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec = corev1.ServiceSpec{
			Selector: map[string]string{
				"app": scalityUIComponent.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		}
		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update Service")
		return ctrl.Result{}, err
	}

	logger.Info("Service reconciled", "result", serviceResult)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
