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
func (r *CommonStatusReconciler) UpdateResourceConditions(statusAware StatusAware,
	conditionUpdates []ConditionUpdate) {

	now := metav1.NewTime(time.Now())
	commonStatus := statusAware.GetCommonStatus()

	for _, update := range conditionUpdates {
		r.setResourceCondition(commonStatus, update.Type, update.Status, update.Reason, update.Message, now)
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

// setResourceCondition sets a condition on a CommonStatus
func (r *CommonStatusReconciler) setResourceCondition(commonStatus *uiscalitycomv1alpha1.CommonStatus,
	conditionType string, status metav1.ConditionStatus, reason, message string, now metav1.Time) {

	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		// Note: ObservedGeneration will be set by the specific controller
	}

	meta.SetStatusCondition(&commonStatus.Conditions, condition)
}

// SetResourceConditionWithError is a helper to set error conditions on any StatusAware resource
func (r *CommonStatusReconciler) SetResourceConditionWithError(statusAware StatusAware,
	conditionType, reason string, err error, observedGeneration int64) {

	now := metav1.NewTime(time.Now())
	message := "Operation successful"
	status := metav1.ConditionTrue

	if err != nil {
		message = err.Error()
		status = metav1.ConditionFalse
	}

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

// ConditionUpdate represents a condition update to be applied
type ConditionUpdate struct {
	Type    string
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}
