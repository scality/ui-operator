package scalityuicomponent

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/scality/reconciler-framework/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

type reconcileContext struct {
	reconciler.BaseReconcileState
}

var _ reconciler.State = &reconcileContext{}

func newReconcileContextWithCtx(ctx context.Context) *reconcileContext {
	return &reconcileContext{
		BaseReconcileState: *reconciler.NewBaseReconcileStateWithCtx(ctx),
	}
}

// Type aliases for better readability
type ScalityUIComponent = *uiv1alpha1.ScalityUIComponent
type State = reconciler.State

// StateReducer represents a step in the reconciliation process
type StateReducer struct {
	N string                                                                 // Name of the reducer
	F func(ScalityUIComponent, State, logr.Logger) (reconcile.Result, error) // Function to execute
}

// asStateReducer creates a StateReducer from a ResourceReconciler
func asStateReducer[R reconciler.Resource](r *ScalityUIComponentReconciler, reconcilerFactory func(ScalityUIComponent, State) reconciler.ResourceReconciler[R], name string) StateReducer {
	return StateReducer{
		F: func(cr ScalityUIComponent, currentState State, log logr.Logger) (reconcile.Result, error) {
			resourceReconciler := reconcilerFactory(cr, currentState)
			return reconciler.ReconcileResource(&r.BaseReconciler, cr, currentState, resourceReconciler, log)
		},
		N: name,
	}
}
