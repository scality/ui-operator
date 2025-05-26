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
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// Constants for the controller
const (
	// ConfigMap related constants
	configMapKey        = "runtime-app-configuration"
	configMapNameSuffix = "runtime-app-configuration"

	// Condition types
	conditionTypeConfigMapReady    = "ConfigMapReady"
	conditionTypeDependenciesReady = "DependenciesReady"

	// Condition reasons
	reasonReconcileSucceeded = "ReconcileSucceeded"
	reasonReconcileFailed    = "ReconcileFailed"
	reasonDependencyMissing  = "DependencyMissing"

	// Requeue intervals
	defaultRequeueInterval = time.Minute

	// Runtime configuration constants
	runtimeConfigKind       = "MicroAppRuntimeConfiguration"
	runtimeConfigAPIVersion = "ui.scality.com/v1alpha1"

	// Default auth configuration for when no auth is specified
	defaultAuthKind       = "OIDC"
	defaultRedirectURL    = "/"
	defaultResponseType   = "code"
	defaultScopes         = "openid email profile"
	defaultProviderLogout = true
	defaultProviderURL    = ""
	defaultClientID       = ""
)

// ScalityUIComponentExposerReconciler reconciles a ScalityUIComponentExposer object
type ScalityUIComponentExposerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// MicroAppRuntimeConfiguration represents the runtime configuration structure
type MicroAppRuntimeConfiguration struct {
	Kind       string                               `json:"kind"`
	APIVersion string                               `json:"apiVersion"`
	Metadata   MicroAppRuntimeConfigurationMetadata `json:"metadata"`
	Spec       MicroAppRuntimeConfigurationSpec     `json:"spec"`
}

// MicroAppRuntimeConfigurationMetadata represents the metadata section
type MicroAppRuntimeConfigurationMetadata struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// MicroAppRuntimeConfigurationSpec represents the spec section
type MicroAppRuntimeConfigurationSpec struct {
	ScalityUI          string                 `json:"scalityUI"`
	ScalityUIComponent string                 `json:"scalityUIComponent"`
	AppHistoryBasePath string                 `json:"appHistoryBasePath,omitempty"`
	Auth               map[string]interface{} `json:"auth,omitempty"`
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalityUIComponentExposerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("exposer", req.NamespacedName)
	logger.Info("Starting reconciliation")

	// Fetch the ScalityUIComponentExposer instance
	exposer := &uiv1alpha1.ScalityUIComponentExposer{}
	if err := r.Get(ctx, req.NamespacedName, exposer); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUIComponentExposer resource not found, assuming it was deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponentExposer")
		return ctrl.Result{}, fmt.Errorf("failed to get ScalityUIComponentExposer: %w", err)
	}

	// Initialize status conditions if needed
	r.initializeStatusConditions(exposer)

	// Validate and fetch dependencies
	component, ui, err := r.validateAndFetchDependencies(ctx, exposer, logger)
	if err != nil {
		return r.handleDependencyError(ctx, exposer, err, logger)
	}

	// Mark dependencies as ready
	r.setStatusCondition(exposer, conditionTypeDependenciesReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "All dependencies are available")

	// Reconcile ConfigMap
	configMap, err := r.reconcileConfigMap(ctx, exposer, ui, component, logger)
	if err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		r.setStatusCondition(exposer, conditionTypeConfigMapReady, metav1.ConditionFalse,
			reasonReconcileFailed, fmt.Sprintf("Failed to reconcile ConfigMap: %v", err))

		if updateErr := r.updateStatus(ctx, exposer, logger); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after ConfigMap reconciliation failure")
		}

		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, fmt.Errorf("failed to reconcile ConfigMap: %w", err)
	}

	// Update status with success
	exposer.Status.RuntimeAppConfigurationConfigMapName = configMap.Name
	r.setStatusCondition(exposer, conditionTypeConfigMapReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "ConfigMap successfully created/updated")

	if err := r.updateStatus(ctx, exposer, logger); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, fmt.Errorf("failed to update status: %w", err)
	}

	logger.Info("Successfully reconciled ScalityUIComponentExposer",
		"configMap", configMap.Name)
	return ctrl.Result{}, nil
}

// initializeStatusConditions initializes status conditions if they don't exist
func (r *ScalityUIComponentExposerReconciler) initializeStatusConditions(exposer *uiv1alpha1.ScalityUIComponentExposer) {
	if exposer.Status.Conditions == nil {
		exposer.Status.Conditions = []metav1.Condition{}
	}
}

// validateAndFetchDependencies validates and fetches the required ScalityUIComponent and ScalityUI resources
func (r *ScalityUIComponentExposerReconciler) validateAndFetchDependencies(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	logger logr.Logger,
) (*uiv1alpha1.ScalityUIComponent, *uiv1alpha1.ScalityUI, error) {

	// Fetch ScalityUIComponent
	component := &uiv1alpha1.ScalityUIComponent{}
	componentKey := types.NamespacedName{
		Name:      exposer.Spec.ScalityUIComponent,
		Namespace: exposer.Namespace,
	}

	if err := r.Get(ctx, componentKey, component); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUIComponent not found", "component", componentKey)
			return nil, nil, fmt.Errorf("ScalityUIComponent %q not found in namespace %q",
				exposer.Spec.ScalityUIComponent, exposer.Namespace)
		}
		return nil, nil, fmt.Errorf("failed to get ScalityUIComponent %q: %w",
			exposer.Spec.ScalityUIComponent, err)
	}

	// Fetch ScalityUI
	ui := &uiv1alpha1.ScalityUI{}
	uiKey := types.NamespacedName{
		Name:      exposer.Spec.ScalityUI,
		Namespace: exposer.Namespace,
	}

	if err := r.Get(ctx, uiKey, ui); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUI not found", "ui", uiKey)
			return nil, nil, fmt.Errorf("ScalityUI %q not found in namespace %q",
				exposer.Spec.ScalityUI, exposer.Namespace)
		}
		return nil, nil, fmt.Errorf("failed to get ScalityUI %q: %w",
			exposer.Spec.ScalityUI, err)
	}

	logger.Info("Successfully validated and fetched dependencies",
		"component", component.Name, "ui", ui.Name)
	return component, ui, nil
}

// handleDependencyError handles errors related to missing dependencies
func (r *ScalityUIComponentExposerReconciler) handleDependencyError(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	err error,
	logger logr.Logger,
) (ctrl.Result, error) {
	logger.Info("Dependency validation failed", "error", err.Error())

	r.setStatusCondition(exposer, conditionTypeDependenciesReady, metav1.ConditionFalse,
		reasonDependencyMissing, err.Error())

	if updateErr := r.updateStatus(ctx, exposer, logger); updateErr != nil {
		logger.Error(updateErr, "Failed to update status after dependency error")
	}

	// Requeue after a longer interval for missing dependencies
	return ctrl.Result{RequeueAfter: defaultRequeueInterval}, nil
}

// setStatusCondition sets a status condition
func (r *ScalityUIComponentExposerReconciler) setStatusCondition(
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	conditionType string,
	status metav1.ConditionStatus,
	reason, message string,
) {
	meta.SetStatusCondition(&exposer.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

// updateStatus updates the status subresource
func (r *ScalityUIComponentExposerReconciler) updateStatus(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	logger logr.Logger,
) error {
	if err := r.Status().Update(ctx, exposer); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}
	logger.Info("Successfully updated status")
	return nil
}

// reconcileConfigMap creates or updates the ConfigMap for a ScalityUIComponentExposer
func (r *ScalityUIComponentExposerReconciler) reconcileConfigMap(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
	logger logr.Logger,
) (*corev1.ConfigMap, error) {
	configMapName := fmt.Sprintf("%s-%s", component.Name, configMapNameSuffix)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: exposer.Namespace,
		},
	}

	logger.Info("Reconciling ConfigMap", "configMap", configMapName)

	result, err := ctrl.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		if err := controllerutil.SetControllerReference(exposer, configMap, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}

		return r.updateConfigMapData(configMap, exposer, ui, component)
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}

	logger.Info("Successfully reconciled ConfigMap",
		"configMap", configMapName, "operation", result)
	return configMap, nil
}

// updateConfigMapData updates the ConfigMap data with the runtime configuration
func (r *ScalityUIComponentExposerReconciler) updateConfigMapData(
	configMap *corev1.ConfigMap,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
) error {
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}

	runtimeConfig := r.buildRuntimeConfiguration(exposer, ui, component)

	configJSONBytes, err := json.MarshalIndent(runtimeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal runtime configuration: %w", err)
	}

	configMap.Data[configMapKey] = string(configJSONBytes)
	return nil
}

// buildRuntimeConfiguration builds the MicroAppRuntimeConfiguration struct
func (r *ScalityUIComponentExposerReconciler) buildRuntimeConfiguration(
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
) MicroAppRuntimeConfiguration {
	return MicroAppRuntimeConfiguration{
		Kind:       runtimeConfigKind,
		APIVersion: runtimeConfigAPIVersion,
		Metadata: MicroAppRuntimeConfigurationMetadata{
			Kind: runtimeConfigKind,
			Name: fmt.Sprintf("%s-exposer", component.Name),
		},
		Spec: MicroAppRuntimeConfigurationSpec{
			ScalityUI:          ui.Name,
			ScalityUIComponent: component.Name,
			AppHistoryBasePath: exposer.Spec.AppHistoryBasePath,
			Auth:               r.buildAuthConfig(exposer.Spec.Auth),
		},
	}
}

// buildAuthConfig builds the authentication configuration
func (r *ScalityUIComponentExposerReconciler) buildAuthConfig(authSpec *uiv1alpha1.AuthConfig) map[string]interface{} {
	if authSpec == nil {
		return map[string]interface{}{
			"kind":           defaultAuthKind,
			"providerUrl":    defaultProviderURL,
			"redirectUrl":    defaultRedirectURL,
			"clientId":       defaultClientID,
			"responseType":   defaultResponseType,
			"scopes":         defaultScopes,
			"providerLogout": defaultProviderLogout,
		}
	}

	authConfig := map[string]interface{}{
		"kind":         authSpec.Kind,
		"providerUrl":  authSpec.ProviderURL,
		"redirectUrl":  authSpec.RedirectURL,
		"clientId":     authSpec.ClientID,
		"responseType": authSpec.ResponseType,
		"scopes":       authSpec.Scopes,
	}

	if authSpec.ProviderLogout != nil {
		authConfig["providerLogout"] = *authSpec.ProviderLogout
	}

	return authConfig
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
