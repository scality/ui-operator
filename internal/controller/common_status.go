package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// StatusAware interface defines methods that resources with status should implement
// This interface is in the controller package to avoid controller-gen issues
type StatusAware interface {
	GetCommonStatus() *uiscalitycomv1alpha1.CommonStatus
	SetCommonStatus(status uiscalitycomv1alpha1.CommonStatus)
}

// CommonStatusReconciler provides generic status reconciliation functionality
type CommonStatusReconciler struct {
	client.Client
}

// NewCommonStatusReconciler creates a new CommonStatusReconciler
func NewCommonStatusReconciler(client client.Client) *CommonStatusReconciler {
	return &CommonStatusReconciler{
		Client: client,
	}
}

// UpdateResourceStatus updates the status of a resource
func (r *CommonStatusReconciler) UpdateResourceStatus(ctx context.Context, obj client.Object) error {
	return r.Status().Update(ctx, obj)
}

// UpdateResourceConditions updates the conditions of a StatusAware resource
// This method automatically uses the object's current generation for observedGeneration
func (r *CommonStatusReconciler) UpdateResourceConditions(obj client.Object, statusAware StatusAware,
	conditionUpdates []ConditionUpdate) {

	for _, update := range conditionUpdates {
		observedGeneration := update.ObservedGeneration
		if observedGeneration == 0 {
			// If not explicitly set, use the object's current generation
			observedGeneration = obj.GetGeneration()
		}
		r.SetResourceCondition(statusAware, update.Type, update.Reason, update.Message, update.Status, observedGeneration)
	}
}

// UpdateResourceConditionsWithGeneration updates the conditions using a specific generation
// Use this when you want to override the automatic generation detection
func (r *CommonStatusReconciler) UpdateResourceConditionsWithGeneration(statusAware StatusAware,
	conditionUpdates []ConditionUpdate, observedGeneration int64) {

	for _, update := range conditionUpdates {
		generation := observedGeneration
		if update.ObservedGeneration != 0 {
			// Allow per-condition override if specified
			generation = update.ObservedGeneration
		}
		r.SetResourceCondition(statusAware, update.Type, update.Reason, update.Message, update.Status, generation)
	}
}

// UpdateResourcePhase updates the phase of a StatusAware resource based on its conditions
func (r *CommonStatusReconciler) UpdateResourcePhase(statusAware StatusAware) {
	commonStatus := statusAware.GetCommonStatus()
	readyCondition := meta.FindStatusCondition(commonStatus.Conditions, uiscalitycomv1alpha1.ConditionTypeReady)
	progressingCondition := meta.FindStatusCondition(commonStatus.Conditions, uiscalitycomv1alpha1.ConditionTypeProgressing)

	if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		commonStatus.Phase = uiscalitycomv1alpha1.PhaseReady
	} else if progressingCondition != nil && progressingCondition.Status == metav1.ConditionTrue {
		commonStatus.Phase = uiscalitycomv1alpha1.PhaseProgressing
	} else if readyCondition != nil && readyCondition.Status == metav1.ConditionFalse {
		if readyCondition.Reason == uiscalitycomv1alpha1.ReasonReconcileError ||
			readyCondition.Reason == uiscalitycomv1alpha1.ReasonConfigurationError {
			commonStatus.Phase = uiscalitycomv1alpha1.PhaseFailed
		} else {
			commonStatus.Phase = uiscalitycomv1alpha1.PhaseProgressing
		}
	} else {
		commonStatus.Phase = uiscalitycomv1alpha1.PhasePending
	}
}

// SetResourceCondition sets a condition on a StatusAware resource with custom message
func (r *CommonStatusReconciler) SetResourceCondition(statusAware StatusAware,
	conditionType, reason, message string, status metav1.ConditionStatus, observedGeneration int64) {

	now := metav1.NewTime(time.Now())
	commonStatus := statusAware.GetCommonStatus()

	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	}

	meta.SetStatusCondition(&commonStatus.Conditions, condition)
}

// SetResourceConditionFromObject sets a condition using the object's current generation
func (r *CommonStatusReconciler) SetResourceConditionFromObject(obj client.Object, statusAware StatusAware,
	conditionType, reason, message string, status metav1.ConditionStatus) {

	r.SetResourceCondition(statusAware, conditionType, reason, message, status, obj.GetGeneration())
}

// SetResourceConditionError sets an error condition on a StatusAware resource
func (r *CommonStatusReconciler) SetResourceConditionError(statusAware StatusAware,
	conditionType, reason string, err error, observedGeneration int64) {

	message := err.Error()
	r.SetResourceCondition(statusAware, conditionType, reason, message, metav1.ConditionFalse, observedGeneration)
}

// SetResourceConditionErrorFromObject sets an error condition using the object's current generation
func (r *CommonStatusReconciler) SetResourceConditionErrorFromObject(obj client.Object, statusAware StatusAware,
	conditionType, reason string, err error) {

	r.SetResourceConditionError(statusAware, conditionType, reason, err, obj.GetGeneration())
}

// SetResourceConditionSuccess sets a success condition on a StatusAware resource
func (r *CommonStatusReconciler) SetResourceConditionSuccess(statusAware StatusAware,
	conditionType, reason, message string, observedGeneration int64) {

	r.SetResourceCondition(statusAware, conditionType, reason, message, metav1.ConditionTrue, observedGeneration)
}

// SetResourceConditionSuccessFromObject sets a success condition using the object's current generation
func (r *CommonStatusReconciler) SetResourceConditionSuccessFromObject(obj client.Object, statusAware StatusAware,
	conditionType, reason, message string) {

	r.SetResourceConditionSuccess(statusAware, conditionType, reason, message, obj.GetGeneration())
}

// ConditionUpdate represents a condition update to be applied
type ConditionUpdate struct {
	Type               string
	Status             metav1.ConditionStatus
	Reason             string
	Message            string
	ObservedGeneration int64 // Optional: if 0, will use the object's current generation
}
