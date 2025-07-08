package scalityui

import (
	"context"

	"github.com/go-logr/logr"
	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newStatusReducer creates a StateReducer for managing the ScalityUI status
func newStatusReducer(cr ScalityUI, currentState State) StateReducer {
	return StateReducer{
		F: func(cr ScalityUI, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			// Update status based on resource states
			return updateStatus(ctx, cr, currentState, log)
		},
		N: "status",
	}
}

// updateStatus updates the ScalityUI status based on the current state of resources
func updateStatus(ctx context.Context, cr ScalityUI, currentState State, log logr.Logger) (reconcile.Result, error) {
	// Get current status
	originalStatus := cr.Status.DeepCopy()

	// Check resource status
	configMapReady := isConfigMapReady(ctx, cr, currentState)
	deploymentReady := isDeploymentReady(ctx, cr, currentState)
	serviceReady := isServiceReady(ctx, cr, currentState)
	ingressReady := isIngressReady(ctx, cr, currentState)

	// Determine overall readiness
	allResourcesReady := configMapReady && deploymentReady && serviceReady && ingressReady

	// Determine phase
	var phase string
	if !configMapReady {
		phase = uiscalitycomv1alpha1.PhasePending
	} else if allResourcesReady {
		phase = uiscalitycomv1alpha1.PhaseReady
	} else {
		phase = uiscalitycomv1alpha1.PhaseProgressing
	}

	// Set Ready condition
	var readyStatus metav1.ConditionStatus
	var readyReason, readyMessage string
	if allResourcesReady {
		readyStatus = metav1.ConditionTrue
		readyReason = "AllResourcesReady"
		readyMessage = "All components are ready"
	} else {
		readyStatus = metav1.ConditionFalse
		readyReason = "ResourcesNotReady"
		if !configMapReady {
			readyMessage = "ConfigMap is not ready"
		} else if !deploymentReady {
			readyMessage = "Deployment is not ready"
		} else if !serviceReady {
			readyMessage = "Service is not ready"
		} else if !ingressReady {
			readyMessage = "Ingress is not ready"
		}
	}

	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               uiscalitycomv1alpha1.ConditionTypeReady,
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMessage,
		LastTransitionTime: metav1.Now(),
	})

	// Set Progressing condition
	var progressingStatus metav1.ConditionStatus
	var progressingReason, progressingMessage string
	if phase == uiscalitycomv1alpha1.PhaseProgressing {
		progressingStatus = metav1.ConditionTrue
		progressingReason = "ResourcesInProgress"
		progressingMessage = "Resources are being created or updated"
	} else {
		progressingStatus = metav1.ConditionFalse
		progressingReason = "ResourcesStable"
		progressingMessage = "All resources are stable"
	}

	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               uiscalitycomv1alpha1.ConditionTypeProgressing,
		Status:             progressingStatus,
		Reason:             progressingReason,
		Message:            progressingMessage,
		LastTransitionTime: metav1.Now(),
	})

	// Update phase
	cr.Status.Phase = phase

	// Only update if status changed
	if originalStatus.Phase != cr.Status.Phase ||
		!meta.IsStatusConditionPresentAndEqual(originalStatus.Conditions, uiscalitycomv1alpha1.ConditionTypeReady, readyStatus) ||
		!meta.IsStatusConditionPresentAndEqual(originalStatus.Conditions, uiscalitycomv1alpha1.ConditionTypeProgressing, progressingStatus) {

		if err := currentState.GetKubeClient().Status().Update(ctx, cr); err != nil {
			log.Error(err, "Failed to update ScalityUI status")
			return reconcile.Result{}, err
		}

		log.Info("ScalityUI status updated", "phase", cr.Status.Phase)
	}

	return reconcile.Result{}, nil
}

// isConfigMapReady checks if the ConfigMap is ready
func isConfigMapReady(ctx context.Context, cr ScalityUI, currentState State) bool {
	configMap := &corev1.ConfigMap{}
	err := currentState.GetKubeClient().Get(ctx, client.ObjectKey{
		Name:      cr.Name,
		Namespace: getOperatorNamespace(),
	}, configMap)
	return err == nil
}

// isDeploymentReady checks if the Deployment is ready
func isDeploymentReady(ctx context.Context, cr ScalityUI, currentState State) bool {
	deployment := &appsv1.Deployment{}
	err := currentState.GetKubeClient().Get(ctx, client.ObjectKey{
		Name:      cr.Name + "-deployment", // Framework generates name with "-deployment" suffix
		Namespace: getOperatorNamespace(),
	}, deployment)
	if err != nil {
		return false
	}

	// Check if deployment has desired replicas ready
	if deployment.Spec.Replicas == nil {
		return false
	}

	desiredReplicas := *deployment.Spec.Replicas
	return deployment.Status.ReadyReplicas == desiredReplicas &&
		deployment.Status.Replicas == desiredReplicas
}

// isServiceReady checks if the Service is ready
func isServiceReady(ctx context.Context, cr ScalityUI, currentState State) bool {
	service := &corev1.Service{}
	err := currentState.GetKubeClient().Get(ctx, client.ObjectKey{
		Name:      cr.Name + "-service", // Framework generates name with "-service" suffix
		Namespace: getOperatorNamespace(),
	}, service)
	return err == nil
}

// isIngressReady checks if the Ingress is ready
func isIngressReady(ctx context.Context, cr ScalityUI, currentState State) bool {
	ingress := &networkingv1.Ingress{}
	err := currentState.GetKubeClient().Get(ctx, client.ObjectKey{
		Name:      cr.Name + "-ingress", // Framework generates name with pattern: name-subresourceType
		Namespace: getOperatorNamespace(),
	}, ingress)
	return err == nil
}
