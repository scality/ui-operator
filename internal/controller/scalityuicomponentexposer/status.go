package scalityuicomponentexposer

import (
	"context"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newStatusReducer creates a StateReducer for managing status using the framework pattern
func newStatusReducer() StateReducer {
	return StateReducer{
		F: func(cr ScalityUIComponentExposer, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			// Update the status based on current conditions
			if err := updateStatus(ctx, cr, currentState, log); err != nil {
				log.Error(err, "Failed to update status")
				return reconcile.Result{RequeueAfter: defaultRequeueInterval}, err
			}

			return reconcile.Result{}, nil
		},
		N: "status",
	}
}

// setStatusCondition sets a condition on the ScalityUIComponentExposer status
func setStatusCondition(cr *uiv1alpha1.ScalityUIComponentExposer, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	meta.SetStatusCondition(&cr.Status.Conditions, condition)
}

// updateStatus updates the ScalityUIComponentExposer status
func updateStatus(ctx context.Context, cr *uiv1alpha1.ScalityUIComponentExposer, currentState State, log logr.Logger) error {
	client := currentState.GetKubeClient()

	// Update the status subresource
	if err := client.Status().Update(ctx, cr); err != nil {
		return err
	}

	log.V(1).Info("Status updated successfully", "exposer", cr.Name)
	return nil
}
