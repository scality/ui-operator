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

package scalityuicomponentexposer

import (
	"context"
	"fmt"

	"github.com/scality/reconciler-framework/reconciler"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ScalityUIComponentExposerReconciler reconciles a ScalityUIComponentExposer object
type ScalityUIComponentExposerReconciler struct {
	reconciler.BaseReconciler[ScalityUIComponentExposer, State]
}

// NewScalityUIComponentExposerReconciler creates a new ScalityUIComponentExposerReconciler
func NewScalityUIComponentExposerReconciler(client client.Client, scheme *runtime.Scheme) *ScalityUIComponentExposerReconciler {
	return &ScalityUIComponentExposerReconciler{
		BaseReconciler: reconciler.BaseReconciler[ScalityUIComponentExposer, State]{
			Client:       client,
			Scheme:       scheme,
			OperatorName: "ui-operator",
		},
	}
}

// NewScalityUIComponentExposerReconcilerForTest creates a ScalityUIComponentExposerReconciler configured for testing environments
func NewScalityUIComponentExposerReconcilerForTest(client client.Client, scheme *runtime.Scheme) *ScalityUIComponentExposerReconciler {
	return &ScalityUIComponentExposerReconciler{
		BaseReconciler: reconciler.BaseReconciler[ScalityUIComponentExposer, State]{
			Client:                   client,
			Scheme:                   scheme,
			OperatorName:             "ui-operator",
			SkipResourceSettledCheck: true, // Skip resource settled check for tests
		},
	}
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/finalizers,verbs=update
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis,verbs=get;list;watch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalityUIComponentExposerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	currentState := newReconcileContextWithCtx(ctx)
	currentState.SetLog(log)
	currentState.SetKubeClient(r.Client)

	cr := &uiv1alpha1.ScalityUIComponentExposer{}
	err := r.Client.Get(ctx, req.NamespacedName, cr)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	resourceReconcilers := buildReducerList(r, cr, currentState)
	for _, rr := range resourceReconcilers {
		res, err := rr.F(cr, currentState, log)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to reconcile %s: %w", rr.N, err)
		}

		if res.Requeue || res.RequeueAfter > 0 {
			return res, nil
		}
	}

	log.Info("reconciliation successful")
	return reconcile.Result{}, nil
}

func buildReducerList(r *ScalityUIComponentExposerReconciler, cr ScalityUIComponentExposer, currentState State) []StateReducer {
	return []StateReducer{
		newDependencyValidationReducer(),
		newConfigMapReducer(r),
		newDeploymentUpdateReducer(r),
		newIngressReducer(r),
		newStatusReducer(),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&networkingv1.Ingress{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.findExposersForConfigMap)).
		Watches(&uiv1alpha1.ScalityUI{}, handler.EnqueueRequestsFromMapFunc(r.findExposersForScalityUI)).
		Watches(&uiv1alpha1.ScalityUIComponent{}, handler.EnqueueRequestsFromMapFunc(r.findExposersForScalityUIComponent)).
		Complete(r)
}
