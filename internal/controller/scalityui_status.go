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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

const (
	PhasePending     = "Pending"
	PhaseProgressing = "Progressing"
	PhaseReady       = "Ready"
	PhaseFailed      = "Failed"
)

const (
	ConditionTypeReady       = "Ready"
	ConditionTypeProgressing = "Progressing"
)

const (
	ReasonReady              = "Ready"
	ReasonNotReady           = "NotReady"
	ReasonProgressing        = "Progressing"
	ReasonReconciling        = "Reconciling"
	ReasonReconcileError     = "ReconcileError"
	ReasonConfigurationError = "ConfigurationError"
	ReasonResourceError      = "ResourceError"
)

// updateScalityUIStatus updates the ScalityUI status
func (r *ScalityUIReconciler) updateScalityUIStatus(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) error {
	// Check the state of resources
	deployment, deploymentErr := r.getScalityUIDeployment(ctx, scalityui)
	service, serviceErr := r.getScalityUIService(ctx, scalityui)
	ingress, ingressErr := r.getScalityUIIngress(ctx, scalityui)

	// Update conditions
	r.updateScalityUIConditions(scalityui, deployment, service, ingress, deploymentErr, serviceErr, ingressErr)

	// Update phase based on conditions
	r.updateScalityUIPhase(scalityui)

	// Update the status
	return r.Status().Update(ctx, scalityui)
}

// updateScalityUIConditions updates the ScalityUI conditions
func (r *ScalityUIReconciler) updateScalityUIConditions(scalityui *uiscalitycomv1alpha1.ScalityUI,
	deployment *appsv1.Deployment, service *corev1.Service, ingress *networkingv1.Ingress,
	deploymentErr, serviceErr, ingressErr error) {

	now := metav1.NewTime(time.Now())

	// Progressing condition
	if deploymentErr != nil || serviceErr != nil || ingressErr != nil {
		setScalityUICondition(scalityui, ConditionTypeProgressing, metav1.ConditionFalse,
			ReasonReconcileError, "Error during resource reconciliation", now)
	} else if deployment != nil && isDeploymentProgressing(deployment) {
		setScalityUICondition(scalityui, ConditionTypeProgressing, metav1.ConditionTrue,
			ReasonProgressing, "Deployment in progress", now)
	} else {
		setScalityUICondition(scalityui, ConditionTypeProgressing, metav1.ConditionFalse,
			ReasonProgressing, "Deployment stable", now)
	}

	// Ready condition
	if deploymentErr != nil || serviceErr != nil || ingressErr != nil {
		setScalityUICondition(scalityui, ConditionTypeReady, metav1.ConditionFalse,
			ReasonNotReady, "Error with resources", now)
	} else if deployment == nil || service == nil || ingress == nil {
		setScalityUICondition(scalityui, ConditionTypeReady, metav1.ConditionFalse,
			ReasonNotReady, "Missing resources", now)
	} else if deployment.Status.ReadyReplicas == 0 {
		setScalityUICondition(scalityui, ConditionTypeReady, metav1.ConditionFalse,
			ReasonNotReady, "No replicas ready", now)
	} else {
		setScalityUICondition(scalityui, ConditionTypeReady, metav1.ConditionTrue,
			ReasonReady, "All components are ready", now)
	}
}

// updateScalityUIPhase updates the phase based on conditions
func (r *ScalityUIReconciler) updateScalityUIPhase(scalityui *uiscalitycomv1alpha1.ScalityUI) {
	readyCondition := meta.FindStatusCondition(scalityui.Status.Conditions, ConditionTypeReady)
	progressingCondition := meta.FindStatusCondition(scalityui.Status.Conditions, ConditionTypeProgressing)

	if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		scalityui.Status.Phase = PhaseReady
	} else if progressingCondition != nil && progressingCondition.Status == metav1.ConditionTrue {
		scalityui.Status.Phase = PhaseProgressing
	} else if readyCondition != nil && readyCondition.Status == metav1.ConditionFalse {
		if readyCondition.Reason == ReasonReconcileError || readyCondition.Reason == ReasonConfigurationError {
			scalityui.Status.Phase = PhaseFailed
		} else {
			scalityui.Status.Phase = PhaseProgressing
		}
	} else {
		scalityui.Status.Phase = PhasePending
	}
}

// Utility functions to retrieve resources
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

// setScalityUICondition sets a condition on the ScalityUI status
func setScalityUICondition(scalityui *uiscalitycomv1alpha1.ScalityUI, conditionType string,
	status metav1.ConditionStatus, reason, message string, now metav1.Time) {

	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: scalityui.Generation,
	}

	meta.SetStatusCondition(&scalityui.Status.Conditions, condition)
}

// setScalityUIConditionWithError is a helper to set error conditions
func (r *ScalityUIReconciler) setScalityUIConditionWithError(scalityui *uiscalitycomv1alpha1.ScalityUI,
	conditionType, reason string, err error) {

	now := metav1.NewTime(time.Now())
	message := "Operation successful"
	status := metav1.ConditionTrue

	if err != nil {
		message = err.Error()
		status = metav1.ConditionFalse
	}

	setScalityUICondition(scalityui, conditionType, status, reason, message, now)
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
