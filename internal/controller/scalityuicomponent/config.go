package scalityuicomponent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newConfigReducer creates a StateReducer for handling configuration fetching and processing
func newConfigReducer(r *ScalityUIComponentReconciler) StateReducer {
	return StateReducer{
		F: func(cr ScalityUIComponent, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			// Check if configuration is already successfully retrieved
			existingCondition := meta.FindStatusCondition(cr.Status.Conditions, "ConfigurationRetrieved")
			if existingCondition != nil && existingCondition.Status == metav1.ConditionTrue {
				log.V(1).Info("Configuration already retrieved, skipping")
				return reconcile.Result{}, nil
			}

			// Also skip if there's a recent failed condition to avoid continuous refetching
			if existingCondition != nil && existingCondition.Status == metav1.ConditionFalse {
				log.V(1).Info("Configuration fetch recently failed, skipping")
				return ctrl.Result{Requeue: true}, nil
			}

			// Re-fetch the Deployment to get its latest status, particularly ReadyReplicas
			// This is crucial for determining if pods are ready before attempting to fetch the UI configuration
			deployment := &appsv1.Deployment{}
			deploymentName := fmt.Sprintf("%s-deployment", cr.Name)
			if err := r.Client.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: cr.Namespace}, deployment); err != nil {
				log.Error(err, "Failed to get deployment status")
				return ctrl.Result{Requeue: true}, nil
			}

			if deployment.Status.ReadyReplicas > 0 {
				// Fetch and process configuration
				return r.processUIComponentConfig(ctx, cr, log)
			} else {
				log.Info("Deployment not ready yet, waiting for pods to start")
				return ctrl.Result{Requeue: true}, nil
			}
		},
		N: "config",
	}
}

// processUIComponentConfig fetches and processes UI component configuration,
// updates the status and returns the reconcile result
func (r *ScalityUIComponentReconciler) processUIComponentConfig(ctx context.Context, scalityUIComponent ScalityUIComponent, logger logr.Logger) (ctrl.Result, error) {
	// Fetch configuration
	configContent, err := r.fetchMicroAppConfig(ctx, scalityUIComponent.Namespace, scalityUIComponent.Name)

	if err != nil {
		logger.Error(err, "Failed to fetch micro-app-configuration")

		// Add a failure condition
		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:               "ConfigurationRetrieved",
			Status:             metav1.ConditionFalse,
			Reason:             "FetchFailed",
			Message:            fmt.Sprintf("Failed to fetch configuration: %v", err),
			LastTransitionTime: metav1.Now(),
		})

		if updateErr := r.Client.Status().Update(ctx, scalityUIComponent); updateErr != nil {
			logger.Error(updateErr, "Failed to update status with failure condition")
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// Parse and apply configuration
	result, err := r.parseAndApplyConfig(ctx, scalityUIComponent, configContent, logger)
	if err != nil {
		return result, nil // Error handling is done in parseAndApplyConfig
	}

	logger.Info("ScalityUIComponent status updated with configuration",
		"kind", scalityUIComponent.Status.Kind,
		"publicPath", scalityUIComponent.Status.PublicPath,
		"version", scalityUIComponent.Status.Version)

	return ctrl.Result{}, nil
}

// parseAndApplyConfig parses the configuration content and updates the ScalityUIComponent status
func (r *ScalityUIComponentReconciler) parseAndApplyConfig(ctx context.Context,
	scalityUIComponent ScalityUIComponent, configContent string, logger logr.Logger) (ctrl.Result, error) {

	var config MicroAppConfig
	if err := json.Unmarshal([]byte(configContent), &config); err != nil {
		logger.Error(err, "Failed to parse micro-app-configuration")

		// Add a failure condition
		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:               "ConfigurationRetrieved",
			Status:             metav1.ConditionFalse,
			Reason:             "ParseFailed",
			Message:            fmt.Sprintf("Failed to parse configuration: %v", err),
			LastTransitionTime: metav1.Now(),
		})

		if updateErr := r.Client.Status().Update(ctx, scalityUIComponent); updateErr != nil {
			logger.Error(updateErr, "Failed to update status with failure condition")
		}

		return ctrl.Result{Requeue: true}, err
	}

	// Update status with retrieved information
	scalityUIComponent.Status.Kind = config.Metadata.Kind
	scalityUIComponent.Status.PublicPath = config.Spec.PublicPath
	scalityUIComponent.Status.Version = config.Spec.Version

	// Add a success condition
	meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
		Type:               "ConfigurationRetrieved",
		Status:             metav1.ConditionTrue,
		Reason:             "FetchSucceeded",
		Message:            "Successfully fetched and applied UI component configuration",
		LastTransitionTime: metav1.Now(),
	})

	// Update the status
	if err := r.Client.Status().Update(ctx, scalityUIComponent); err != nil {
		logger.Error(err, "Failed to update status with configuration")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
