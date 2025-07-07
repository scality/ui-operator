package scalityui

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	controller "github.com/scality/ui-operator/internal/controller"
)

// updateScalityUIStatus updates the ScalityUI status using common_status.go infrastructure
func (r *ScalityUIReconciler) updateScalityUIStatus(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) error {
	// Check the state of resources
	deployment, deploymentErr := r.getScalityUIDeployment(ctx, scalityui)
	service, serviceErr := r.getScalityUIService(ctx, scalityui)
	ingress, ingressErr := r.getScalityUIIngress(ctx, scalityui)

	// Create common status reconciler
	commonStatusReconciler := controller.NewCommonStatusReconciler(r.Client)

	// Build condition updates using helper functions
	var conditionUpdates []controller.ConditionUpdate

	// Add Progressing condition
	conditionUpdates = append(conditionUpdates, r.buildProgressingCondition(deployment, service, ingress, deploymentErr, serviceErr, ingressErr))

	// Add Ready condition
	conditionUpdates = append(conditionUpdates, r.buildReadyCondition(deployment, service, ingress, deploymentErr, serviceErr, ingressErr))

	// Use common status reconciler to update conditions (using existing Status methods!)
	commonStatusReconciler.UpdateResourceConditions(scalityui, &scalityui.Status, conditionUpdates)

	// Use common status reconciler to update phase (using existing Status methods!)
	commonStatusReconciler.UpdateResourcePhase(&scalityui.Status)

	// Use common status reconciler to update status (reusing existing infrastructure!)
	return commonStatusReconciler.UpdateResourceStatus(ctx, scalityui)
}

// buildProgressingCondition builds the Progressing condition based on resource state
func (r *ScalityUIReconciler) buildProgressingCondition(deployment *appsv1.Deployment, service *corev1.Service, ingress *networkingv1.Ingress, deploymentErr, serviceErr, ingressErr error) controller.ConditionUpdate {
	// Check for common errors/missing resources
	if commonCondition := r.checkCommonResourceIssues(uiscalitycomv1alpha1.ConditionTypeProgressing, deployment, service, ingress, deploymentErr, serviceErr, ingressErr); commonCondition != nil {
		return *commonCondition
	}

	// Check if deployment is progressing
	if isDeploymentProgressing(deployment) {
		return controller.ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  uiscalitycomv1alpha1.ReasonProgressing,
			Message: "Deployment in progress",
		}
	}

	// Deployment is stable
	return controller.ConditionUpdate{
		Type:    uiscalitycomv1alpha1.ConditionTypeProgressing,
		Status:  metav1.ConditionFalse,
		Reason:  uiscalitycomv1alpha1.ReasonProgressing,
		Message: "Deployment stable",
	}
}

// buildReadyCondition builds the Ready condition based on resource state
func (r *ScalityUIReconciler) buildReadyCondition(deployment *appsv1.Deployment, service *corev1.Service, ingress *networkingv1.Ingress, deploymentErr, serviceErr, ingressErr error) controller.ConditionUpdate {
	// Check for common errors/missing resources
	if commonCondition := r.checkCommonResourceIssues(uiscalitycomv1alpha1.ConditionTypeReady, deployment, service, ingress, deploymentErr, serviceErr, ingressErr); commonCondition != nil {
		return *commonCondition
	}

	// Check if deployment has ready replicas
	if deployment.Status.ReadyReplicas == 0 {
		return controller.ConditionUpdate{
			Type:    uiscalitycomv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  uiscalitycomv1alpha1.ReasonNotReady,
			Message: "No replicas ready",
		}
	}

	// All checks passed - ready
	return controller.ConditionUpdate{
		Type:    uiscalitycomv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  uiscalitycomv1alpha1.ReasonReady,
		Message: "All components are ready",
	}
}

// checkCommonResourceIssues checks for common resource errors and missing resources
// Returns nil if no common issues found, otherwise returns the appropriate condition
func (r *ScalityUIReconciler) checkCommonResourceIssues(conditionType string, deployment *appsv1.Deployment, service *corev1.Service, ingress *networkingv1.Ingress, deploymentErr, serviceErr, ingressErr error) *controller.ConditionUpdate {
	// Check for errors first
	if deploymentErr != nil || serviceErr != nil || ingressErr != nil {
		reason := uiscalitycomv1alpha1.ReasonReconcileError
		message := "Error during resource reconciliation"
		if conditionType == uiscalitycomv1alpha1.ConditionTypeReady {
			reason = uiscalitycomv1alpha1.ReasonNotReady
			message = "Error with resources"
		}
		return &controller.ConditionUpdate{
			Type:    conditionType,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		}
	}

	// Check for missing resources
	if deployment == nil || service == nil || ingress == nil {
		reason := uiscalitycomv1alpha1.ReasonMissingResources
		message := "Missing required resources"
		if conditionType == uiscalitycomv1alpha1.ConditionTypeReady {
			reason = uiscalitycomv1alpha1.ReasonNotReady
			message = "Missing resources"
		}
		return &controller.ConditionUpdate{
			Type:    conditionType,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		}
	}

	return nil // No common issues found
}

// Generic utility function to retrieve Kubernetes resources
// Returns (found, error) where found indicates if the resource exists
func (r *ScalityUIReconciler) getResource(ctx context.Context, name, namespace string, obj client.Object) (bool, error) {
	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, obj)

	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil // Resource not found, not an error
		}
		return false, err // Real error occurred
	}
	return true, nil // Resource found successfully
}

// Utility functions to retrieve resources using the generic helper
func (r *ScalityUIReconciler) getScalityUIDeployment(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	found, err := r.getResource(ctx, scalityui.Name, getOperatorNamespace(), deployment)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return deployment, nil
}

func (r *ScalityUIReconciler) getScalityUIService(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (*corev1.Service, error) {
	service := &corev1.Service{}
	found, err := r.getResource(ctx, scalityui.Name, getOperatorNamespace(), service)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return service, nil
}

func (r *ScalityUIReconciler) getScalityUIIngress(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (*networkingv1.Ingress, error) {
	ingress := &networkingv1.Ingress{}
	found, err := r.getResource(ctx, scalityui.Name, getOperatorNamespace(), ingress)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return ingress, nil
}

// isDeploymentProgressing checks if the deployment is progressing
func isDeploymentProgressing(deployment *appsv1.Deployment) bool {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
