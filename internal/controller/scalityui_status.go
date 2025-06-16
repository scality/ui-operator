package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// Note: Constants have been moved to api/v1alpha1/common_status.go for reusability

// updateScalityUIStatus updates the ScalityUI status using the common status reconciler
func (r *ScalityUIReconciler) updateScalityUIStatus(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) error {
	// Check the state of resources
	deployment, deploymentErr := r.getScalityUIDeployment(ctx, scalityui)
	service, serviceErr := r.getScalityUIService(ctx, scalityui)
	ingress, ingressErr := r.getScalityUIIngress(ctx, scalityui)

	// Create common status reconciler
	commonStatusReconciler := NewCommonStatusReconciler(r.Client)

	// Update conditions using the common reconciler
	r.updateScalityUIConditionsWithCommonReconciler(scalityui, deployment, service, ingress,
		deploymentErr, serviceErr, ingressErr, commonStatusReconciler)

	// Update phase based on conditions using the common reconciler
	commonStatusReconciler.UpdateResourcePhase(&scalityui.Status)

	// Update the status
	return commonStatusReconciler.UpdateResourceStatus(ctx, scalityui)
}

// updateScalityUIConditionsWithCommonReconciler updates the ScalityUI conditions using the common reconciler
func (r *ScalityUIReconciler) updateScalityUIConditionsWithCommonReconciler(scalityui *uiscalitycomv1alpha1.ScalityUI,
	deployment *appsv1.Deployment, service *corev1.Service, ingress *networkingv1.Ingress,
	deploymentErr, serviceErr, ingressErr error, commonStatusReconciler *CommonStatusReconciler) {

	var conditionUpdates []ConditionUpdate

	// Progressing condition
	if deploymentErr != nil || serviceErr != nil || ingressErr != nil {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  uiscalitycomv1alpha1.ReasonReconcileError,
			Message: "Error during resource reconciliation",
		})
	} else if deployment != nil && isDeploymentProgressing(deployment) {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  uiscalitycomv1alpha1.ReasonProgressing,
			Message: "Deployment in progress",
		})
	} else {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  uiscalitycomv1alpha1.ReasonProgressing,
			Message: "Deployment stable",
		})
	}

	// Ready condition
	if deploymentErr != nil || serviceErr != nil || ingressErr != nil {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  uiscalitycomv1alpha1.ReasonNotReady,
			Message: "Error with resources",
		})
	} else if deployment == nil || service == nil || ingress == nil {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  uiscalitycomv1alpha1.ReasonNotReady,
			Message: "Missing resources",
		})
	} else if deployment.Status.ReadyReplicas == 0 {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  uiscalitycomv1alpha1.ReasonNotReady,
			Message: "No replicas ready",
		})
	} else {
		conditionUpdates = append(conditionUpdates, ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  uiscalitycomv1alpha1.ReasonReady,
			Message: "All components are ready",
		})
	}

	// Apply condition updates with ObservedGeneration
	for _, update := range conditionUpdates {
		commonStatusReconciler.SetResourceConditionWithError(&scalityui.Status,
			update.Type, update.Reason, nil, scalityui.Generation)

		// Override the condition with the correct status and message
		commonStatus := scalityui.Status.GetCommonStatus()
		for i := range commonStatus.Conditions {
			if commonStatus.Conditions[i].Type == update.Type {
				commonStatus.Conditions[i].Status = update.Status
				commonStatus.Conditions[i].Message = update.Message
				break
			}
		}
	}
}

// Utility functions to retrieve resources (kept as ScalityUI-specific)
func (r *ScalityUIReconciler) getScalityUIDeployment(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      scalityui.Name,
		Namespace: getOperatorNamespace(),
	}, deployment)

	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if err != nil {
		return nil, nil // Not found
	}
	return deployment, nil
}

func (r *ScalityUIReconciler) getScalityUIService(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      scalityui.Name,
		Namespace: getOperatorNamespace(),
	}, service)

	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if err != nil {
		return nil, nil // Not found
	}
	return service, nil
}

func (r *ScalityUIReconciler) getScalityUIIngress(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (*networkingv1.Ingress, error) {
	ingress := &networkingv1.Ingress{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      scalityui.Name,
		Namespace: getOperatorNamespace(),
	}, ingress)

	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if err != nil {
		return nil, nil // Not found
	}
	return ingress, nil
}

// setScalityUIConditionWithError is a helper to set error conditions (kept for backward compatibility)
func (r *ScalityUIReconciler) setScalityUIConditionWithError(scalityui *uiscalitycomv1alpha1.ScalityUI,
	conditionType, reason string, err error) {

	commonStatusReconciler := NewCommonStatusReconciler(r.Client)
	commonStatusReconciler.SetResourceConditionWithError(&scalityui.Status, conditionType, reason, err, scalityui.Generation)
}

// handleReconcileError is a helper function that handles reconciliation errors (kept for backward compatibility)
func (r *ScalityUIReconciler) handleReconcileError(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI, conditionType, reason string, err error) {
	r.setScalityUIConditionWithError(scalityui, conditionType, reason, err)
	if statusErr := r.updateScalityUIStatus(ctx, scalityui); statusErr != nil {
		r.Log.Error(statusErr, "Failed to update status after error", "originalError", err, "reason", reason)
	}
}

// isDeploymentProgressing checks if the deployment is progressing (kept as utility function)
func isDeploymentProgressing(deployment *appsv1.Deployment) bool {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
