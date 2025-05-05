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

	"github.com/go-logr/logr"
	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// ScalityUIReconciler reconciles a ScalityUI object
type ScalityUIReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=ui.scality.com.ui-operator.scality.com,resources=scalityuis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com.ui-operator.scality.com,resources=scalityuis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com.ui-operator.scality.com,resources=scalityuis/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ScalityUI object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *ScalityUIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// explain this line
	_ = log.FromContext(ctx)

	r.Log.Info("Reconcile", "request", req)

	scalityui := &uiscalitycomv1alpha1.ScalityUI{}
	err := r.Client.Get(ctx, req.NamespacedName, scalityui)

	if err != nil {
		r.Log.Error(err, "Failed to get ScalityUI")
		return ctrl.Result{}, err
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testdeploy",
			Namespace: "ui",
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "testdeploy"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "testdeploy"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "shell-ui", Image: scalityui.Spec.ShellUIimage},
					},
				},
			},
		}
		return nil
	})

	if err != nil {
		r.Log.Error(err, "Failed to create or update deployment")
		return ctrl.Result{}, err
	}

	// Log the operation result
	switch op {
	case controllerutil.OperationResultCreated:
		r.Log.Info("Deployment created", "name", deploy.Name)
	case controllerutil.OperationResultUpdated:
		r.Log.Info("Deployment updated", "name", deploy.Name)
	case controllerutil.OperationResultNone:
		r.Log.Info("Deployment unchanged", "name", deploy.Name)
	}

	r.Log.Info("ScalityUI", "debug", scalityui.Spec.ShellUIimage)
	r.Log.Info("ctx", "ctx", ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiscalitycomv1alpha1.ScalityUI{}).
		Complete(r)
}
