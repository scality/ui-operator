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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// Constants for the controller
const (
	// ConfigMap related constants
	configMapNameSuffix = "runtime-app-configuration"

	// Condition types
	conditionTypeConfigMapReady    = "ConfigMapReady"
	conditionTypeDependenciesReady = "DependenciesReady"
	conditionTypeDeploymentReady   = "DeploymentReady"

	// Condition reasons
	reasonReconcileSucceeded = "ReconcileSucceeded"
	reasonReconcileFailed    = "ReconcileFailed"
	reasonDependencyMissing  = "DependencyMissing"

	// Requeue intervals
	defaultRequeueInterval = time.Minute

	// Runtime configuration constants
	runtimeConfigKind       = "MicroAppRuntimeConfiguration"
	runtimeConfigAPIVersion = "ui.scality.com/v1alpha1"

	// Mount and deployment related constants
	configHashAnnotation = "ui.scality.com/config-hash"
	volumeNamePrefix     = "config-volume-"

	// Finalizers
	exposerFinalizer         = "uicomponentexposer.scality.com/finalizer"
	configMapFinalizerPrefix = "uicomponentexposer.scality.com/"
)

// ScalityUIComponentExposerReconciler reconciles a ScalityUIComponentExposer object
type ScalityUIComponentExposerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/finalizers,verbs=update
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis,verbs=get;list;watch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis,verbs=get;list;watch,resourceNames=*
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

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
	SelfConfiguration  map[string]interface{} `json:"selfConfiguration,omitempty"`
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

	// Handle deletion and finalizer logic
	if !exposer.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(exposer, exposerFinalizer) {
			// Run finalization logic
			if err := r.finalizeExposer(ctx, exposer, logger); err != nil {
				logger.Error(err, "Failed to finalize ScalityUIComponentExposer")
				return ctrl.Result{}, err
			}

			// Remove our finalizer and update the object
			controllerutil.RemoveFinalizer(exposer, exposerFinalizer)
			if err := r.Update(ctx, exposer); err != nil {
				logger.Error(err, "Failed to remove finalizer from ScalityUIComponentExposer")
				return ctrl.Result{}, err
			}
		}
		// Nothing more to do since the object is being deleted
		logger.Info("ScalityUIComponentExposer deletion reconciled")
		return ctrl.Result{}, nil
	}

	// Ensure the exposer has our finalizer for future clean-up
	if !controllerutil.ContainsFinalizer(exposer, exposerFinalizer) {
		controllerutil.AddFinalizer(exposer, exposerFinalizer)
		if err := r.Update(ctx, exposer); err != nil {
			logger.Error(err, "Failed to add finalizer to ScalityUIComponentExposer")
			return ctrl.Result{}, err
		}
		// Continue reconciliation with the finalizer now set
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

	r.setStatusCondition(exposer, conditionTypeConfigMapReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "ConfigMap successfully created/updated")

	// Calculate ConfigMap hash and update deployment
	configMapHash, err := r.calculateConfigMapHash(configMap)
	if err != nil {
		logger.Error(err, "Failed to calculate ConfigMap hash")
		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, fmt.Errorf("failed to calculate ConfigMap hash: %w", err)
	}

	if err := r.updateComponentDeployment(ctx, component, exposer, configMapHash, logger); err != nil {
		logger.Error(err, "Failed to update component deployment")
		r.setStatusCondition(exposer, conditionTypeDeploymentReady, metav1.ConditionFalse,
			reasonReconcileFailed, fmt.Sprintf("Failed to update deployment: %v", err))

		if updateErr := r.updateStatus(ctx, exposer, logger); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after deployment update failure")
		}

		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, fmt.Errorf("failed to update component deployment: %w", err)
	}

	r.setStatusCondition(exposer, conditionTypeDeploymentReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "Deployment successfully updated with ConfigMap mount")

	// Reconcile Ingress (inherits from ScalityUI Networks, can be overridden by exposer config)
	if err := r.reconcileIngress(ctx, exposer, component, ui, logger); err != nil {
		logger.Error(err, "Failed to reconcile Ingress")
		r.setStatusCondition(exposer, "IngressReady", metav1.ConditionFalse,
			reasonReconcileFailed, fmt.Sprintf("Failed to reconcile Ingress: %v", err))

		if updateErr := r.updateStatus(ctx, exposer, logger); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after Ingress reconciliation failure")
		}

		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, fmt.Errorf("failed to reconcile Ingress: %w", err)
	}

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

	// Fetch ScalityUI (cluster-scoped resource)
	ui := &uiv1alpha1.ScalityUI{}
	uiKey := types.NamespacedName{
		Name: exposer.Spec.ScalityUI,
	}

	if err := r.Get(ctx, uiKey, ui); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUI not found", "ui", uiKey)
			return nil, nil, fmt.Errorf("ScalityUI %q not found", exposer.Spec.ScalityUI)
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
		// Add exposer-specific finalizer to the ConfigMap so it is only deleted when all exposers are gone
		finalizerName := configMapFinalizerPrefix + exposer.Name
		if !controllerutil.ContainsFinalizer(configMap, finalizerName) {
			controllerutil.AddFinalizer(configMap, finalizerName)
		}

		// Build and add the runtime configuration for this exposer
		if err := r.updateConfigMapData(configMap, exposer, ui, component); err != nil {
			return err
		}

		return nil
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

	runtimeConfig, err := r.buildRuntimeConfiguration(exposer, ui, component)
	if err != nil {
		return fmt.Errorf("failed to build runtime configuration: %w", err)
	}

	configJSONBytes, err := json.MarshalIndent(runtimeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal runtime configuration: %w", err)
	}

	// Use exposer name as the key in ConfigMap
	configMap.Data[exposer.Name] = string(configJSONBytes)
	return nil
}

// buildRuntimeConfiguration builds the MicroAppRuntimeConfiguration struct
func (r *ScalityUIComponentExposerReconciler) buildRuntimeConfiguration(
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
) (MicroAppRuntimeConfiguration, error) {
	authConfig, err := r.buildAuthConfig(exposer.Spec.Auth)
	if err != nil {
		return MicroAppRuntimeConfiguration{}, fmt.Errorf("failed to build auth config: %w", err)
	}

	// Parse SelfConfiguration from RawExtension
	var selfConfig map[string]interface{}
	if exposer.Spec.SelfConfiguration != nil && exposer.Spec.SelfConfiguration.Raw != nil {
		if err := json.Unmarshal(exposer.Spec.SelfConfiguration.Raw, &selfConfig); err != nil {
			return MicroAppRuntimeConfiguration{}, fmt.Errorf("failed to unmarshal self configuration: %w", err)
		}
	}

	return MicroAppRuntimeConfiguration{
		Kind:       runtimeConfigKind,
		APIVersion: runtimeConfigAPIVersion,
		Metadata: MicroAppRuntimeConfigurationMetadata{
			Kind: component.Status.Kind,
			Name: component.Name,
		},
		Spec: MicroAppRuntimeConfigurationSpec{
			ScalityUI:          ui.Name,
			ScalityUIComponent: component.Name,
			AppHistoryBasePath: exposer.Spec.AppHistoryBasePath,
			Auth:               authConfig,
			SelfConfiguration:  selfConfig,
		},
	}, nil
}

// buildAuthConfig builds the authentication configuration
func (r *ScalityUIComponentExposerReconciler) buildAuthConfig(authSpec *uiv1alpha1.AuthConfig) (map[string]interface{}, error) {
	// If exposer has auth config, validate it's complete
	if authSpec != nil {
		if err := r.validateAuthConfig(authSpec); err != nil {
			return nil, fmt.Errorf("exposer auth configuration incomplete: %w", err)
		}
		return r.authConfigToMap(authSpec), nil
	}

	// No auth configuration available - ScalityUI auth will be implemented in another branch
	return map[string]interface{}{}, nil
}

// validateAuthConfig validates that all required auth fields are present
func (r *ScalityUIComponentExposerReconciler) validateAuthConfig(auth *uiv1alpha1.AuthConfig) error {
	requiredFields := map[string]string{
		"kind":         auth.Kind,
		"providerUrl":  auth.ProviderURL,
		"redirectUrl":  auth.RedirectURL,
		"clientId":     auth.ClientID,
		"responseType": auth.ResponseType,
		"scopes":       auth.Scopes,
	}

	for field, value := range requiredFields {
		if value == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	return nil
}

// authConfigToMap converts AuthConfig to map[string]interface{}
func (r *ScalityUIComponentExposerReconciler) authConfigToMap(auth *uiv1alpha1.AuthConfig) map[string]interface{} {
	authConfig := map[string]interface{}{
		"kind":         auth.Kind,
		"providerUrl":  auth.ProviderURL,
		"redirectUrl":  auth.RedirectURL,
		"clientId":     auth.ClientID,
		"responseType": auth.ResponseType,
		"scopes":       auth.Scopes,
	}

	if auth.ProviderLogout != nil {
		authConfig["providerLogout"] = *auth.ProviderLogout
	}

	return authConfig
}

// findExposersForScalityUI finds all ScalityUIComponentExposer resources that reference a given ScalityUI
func (r *ScalityUIComponentExposerReconciler) findExposersForScalityUI(ctx context.Context, obj client.Object) []reconcile.Request {
	scalityUI, ok := obj.(*uiv1alpha1.ScalityUI)
	if !ok {
		return nil
	}

	// List all ScalityUIComponentExposer resources
	exposerList := &uiv1alpha1.ScalityUIComponentExposerList{}
	if err := r.List(ctx, exposerList); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUI == scalityUI.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposer.Name,
					Namespace: exposer.Namespace,
				},
			})
		}
	}

	return requests
}

// findExposersForScalityUIComponent finds all ScalityUIComponentExposer resources that reference a given ScalityUIComponent
func (r *ScalityUIComponentExposerReconciler) findExposersForScalityUIComponent(ctx context.Context, obj client.Object) []reconcile.Request {
	component, ok := obj.(*uiv1alpha1.ScalityUIComponent)
	if !ok {
		return nil
	}

	// List all ScalityUIComponentExposer resources in the same namespace
	exposerList := &uiv1alpha1.ScalityUIComponentExposerList{}
	if err := r.List(ctx, exposerList, client.InNamespace(component.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUIComponent == component.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      exposer.Name,
					Namespace: exposer.Namespace,
				},
			})
		}
	}

	return requests
}

// findExposersForConfigMap maps a ConfigMap event to related ScalityUIComponentExposer reconcile requests
func (r *ScalityUIComponentExposerReconciler) findExposersForConfigMap(ctx context.Context, obj client.Object) []reconcile.Request {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}

	var requests []reconcile.Request

	// We expect ConfigMap name format: <component-name>-runtime-app-configuration
	if !strings.HasSuffix(cm.Name, configMapNameSuffix) {
		return nil
	}

	componentName := strings.TrimSuffix(cm.Name, "-"+configMapNameSuffix)

	// List all exposers in the same namespace that reference this component
	exposerList := &uiv1alpha1.ScalityUIComponentExposerList{}
	if err := r.List(ctx, exposerList, client.InNamespace(cm.Namespace)); err != nil {
		return nil
	}

	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUIComponent == componentName {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: exposer.Name, Namespace: exposer.Namespace},
			})
		}
	}
	return requests
}

// calculateConfigMapHash calculates the SHA-256 hash of the ConfigMap data
func (r *ScalityUIComponentExposerReconciler) calculateConfigMapHash(configMap *corev1.ConfigMap) (string, error) {
	// Hash all data in the ConfigMap to ensure any changes trigger deployment update
	if len(configMap.Data) == 0 {
		return "", fmt.Errorf("ConfigMap '%s' has no data", configMap.Name)
	}

	// Create a sorted representation of all data for consistent hashing
	var dataStrings []string
	for key, value := range configMap.Data {
		dataStrings = append(dataStrings, fmt.Sprintf("%s=%s", key, value))
	}

	// Sort to ensure consistent hash regardless of map iteration order
	for i := 0; i < len(dataStrings); i++ {
		for j := i + 1; j < len(dataStrings); j++ {
			if dataStrings[i] > dataStrings[j] {
				dataStrings[i], dataStrings[j] = dataStrings[j], dataStrings[i]
			}
		}
	}

	combinedData := strings.Join(dataStrings, "|")
	hash := sha256.Sum256([]byte(combinedData))
	return hex.EncodeToString(hash[:]), nil
}

// updateComponentDeployment updates the component's deployment to mount the ConfigMap
func (r *ScalityUIComponentExposerReconciler) updateComponentDeployment(
	ctx context.Context,
	component *uiv1alpha1.ScalityUIComponent,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	configMapHash string,
	logger logr.Logger,
) error {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      component.Name,
		Namespace: exposer.Namespace,
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Component deployment not found, skipping update",
				"deployment", component.Name)
			return nil
		}
		return fmt.Errorf("failed to get component deployment: %w", err)
	}

	logger.Info("Updating deployment with ConfigMap mount", "deployment", deployment.Name)

	configChanged := false

	result, err := ctrl.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		volumeName := volumeNamePrefix + component.Name
		configMapName := fmt.Sprintf("%s-%s", component.Name, configMapNameSuffix)

		// Update or add the ConfigMap volume
		configChanged = r.ensureConfigMapVolume(deployment, volumeName, configMapName) || configChanged

		// Update or add the ConfigMap volume mount for each container
		// Mount to configs subdirectory to avoid overwriting the original micro-app-configuration file
		configsMountPath := component.Spec.MountPath + "/configs"
		for i := range deployment.Spec.Template.Spec.Containers {
			configChanged = r.ensureConfigMapVolumeMount(&deployment.Spec.Template.Spec.Containers[i], volumeName, configsMountPath) || configChanged
		}

		// Set annotation to trigger rolling update if configuration changed or hash is different
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}

		// Update annotation if config changed or if hash is different
		currentHashAnnotation := deployment.Spec.Template.Annotations[configHashAnnotation]

		currentHash := ""
		if currentHashAnnotation != "" {
			if idx := strings.Index(currentHashAnnotation, "-"); idx > 0 {
				currentHash = currentHashAnnotation[:idx]
			} else {
				currentHash = currentHashAnnotation
			}
		}

		if configChanged || currentHash != configMapHash {
			// Force a unique value by appending a timestamp to ensure pod restart
			timestamp := time.Now().Format(time.RFC3339)
			deployment.Spec.Template.Annotations[configHashAnnotation] = configMapHash + "-" + timestamp
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update deployment with ConfigMap mount: %w", err)
	}

	logger.Info("Successfully updated deployment with ConfigMap mount",
		"deployment", deployment.Name, "configChanged", configChanged, "operation", result)
	return nil
}

// ensureConfigMapVolume ensures the deployment has the specified ConfigMap volume
// Returns true if the volume was added or modified
func (r *ScalityUIComponentExposerReconciler) ensureConfigMapVolume(deployment *appsv1.Deployment, volumeName, configMapName string) bool {
	for i, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName {
			// Update existing volume if needed
			if vol.ConfigMap == nil || vol.ConfigMap.Name != configMapName {
				deployment.Spec.Template.Spec.Volumes[i].ConfigMap = &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				}
				return true
			}
			return false // Volume exists and is correctly configured
		}
	}

	// Add new volume
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	})
	return true
}

// ensureConfigMapVolumeMount ensures the container has the specified volume mount
// Returns true if the mount was added or modified
func (r *ScalityUIComponentExposerReconciler) ensureConfigMapVolumeMount(container *corev1.Container, volumeName, mountPath string) bool {
	// Check for existing mount and update if needed
	for i, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			// Check if mount configuration needs update (no SubPath for directory mount)
			needsUpdate := mount.MountPath != mountPath ||
				mount.SubPath != "" ||
				!mount.ReadOnly

			if needsUpdate {
				container.VolumeMounts[i].MountPath = mountPath
				container.VolumeMounts[i].SubPath = "" // No subPath for directory mount
				container.VolumeMounts[i].ReadOnly = true
				return true
			}
			return false // Mount exists and is correctly configured
		}
	}

	// Add new mount (directory mount without subPath)
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
		ReadOnly:  true,
	})
	return true
}

// finalizeExposer performs clean-up tasks before the ScalityUIComponentExposer object is removed.
func (r *ScalityUIComponentExposerReconciler) finalizeExposer(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	logger logr.Logger,
) error {
	const maxRetries = 3
	configMapName := fmt.Sprintf("%s-%s", exposer.Spec.ScalityUIComponent, configMapNameSuffix)
	finalizerName := configMapFinalizerPrefix + exposer.Name

	for i := 0; i < maxRetries; i++ {
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: exposer.Namespace}, cm); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("ConfigMap not found during finalization; nothing to clean", "configMap", configMapName)
				return nil
			}
			return fmt.Errorf("failed to get ConfigMap during finalization: %w", err)
		}

		// Remove the exposer-specific data key from ConfigMap
		dataRemoved := false
		if cm.Data != nil {
			if _, exists := cm.Data[exposer.Name]; exists {
				delete(cm.Data, exposer.Name)
				dataRemoved = true
				logger.Info("Removed exposer data from ConfigMap", "exposer", exposer.Name, "configMap", cm.Name)
			}
		}

		// Remove the exposer-specific finalizer from the ConfigMap, if present
		finalizerRemoved := false
		if controllerutil.ContainsFinalizer(cm, finalizerName) {
			controllerutil.RemoveFinalizer(cm, finalizerName)
			finalizerRemoved = true
			logger.Info("Removed finalizer from ConfigMap", "finalizer", finalizerName, "configMap", cm.Name)
		}

		// Update the ConfigMap with removed data and finalizer
		if dataRemoved || finalizerRemoved {
			if err := r.Update(ctx, cm); err != nil {
				if errors.IsConflict(err) && i < maxRetries-1 {
					logger.Info("Conflict updating ConfigMap during finalization, retrying", "attempt", i+1, "configMap", cm.Name)
					time.Sleep(time.Millisecond * time.Duration(100*(i+1))) // exponential backoff
					continue
				}
				return fmt.Errorf("failed to update ConfigMap during finalization: %w", err)
			}
		}

		// If no other finalizers remain and no data exists, delete the ConfigMap altogether
		if len(cm.Finalizers) == 0 && len(cm.Data) == 0 {
			if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
				if errors.IsConflict(err) && i < maxRetries-1 {
					logger.Info("Conflict deleting ConfigMap during finalization, retrying", "attempt", i+1, "configMap", cm.Name)
					time.Sleep(time.Millisecond * time.Duration(100*(i+1))) // exponential backoff
					continue
				}
				return fmt.Errorf("failed to delete ConfigMap during finalization: %w", err)
			}
			logger.Info("Deleted ConfigMap as no finalizers and data remain", "configMap", cm.Name)
		}

		return nil
	}

	return fmt.Errorf("failed to finalize exposer after %d retries", maxRetries)
}

// reconcileIngress creates or updates an Ingress for the exposer if ingress is enabled
func (r *ScalityUIComponentExposerReconciler) reconcileIngress(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	component *uiv1alpha1.ScalityUIComponent,
	ui *uiv1alpha1.ScalityUI,
	logger logr.Logger,
) error {
	// Merge networks configuration (exposer config overrides UI config)
	networksConfig, path := r.mergeNetworksConfig(exposer, ui, component)

	// If networks is not configured, skip ingress creation
	// (any existing ingress will be garbage collected automatically by Kubernetes)
	if networksConfig == nil {
		r.setStatusCondition(exposer, "IngressReady", metav1.ConditionTrue,
			reasonReconcileSucceeded, "Ingress not needed (networks not configured)")
		return nil
	}

	// Create or update the ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exposer.Name,
			Namespace: exposer.Namespace,
		},
	}

	result, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		return r.buildIngress(ingress, exposer, component, networksConfig, path)
	})

	if err != nil {
		return fmt.Errorf("failed to create or update Ingress: %w", err)
	}

	logger.Info("Successfully reconciled Ingress", "ingress", ingress.Name, "operation", result)
	r.setStatusCondition(exposer, "IngressReady", metav1.ConditionTrue,
		reasonReconcileSucceeded, "Ingress successfully created/updated")

	return nil
}

// mergeNetworksConfig merges networks configuration with inheritance and override logic
func (r *ScalityUIComponentExposerReconciler) mergeNetworksConfig(
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
) (*uiv1alpha1.UINetworks, string) {
	// Start with default configuration
	merged := &uiv1alpha1.UINetworks{}
	var path string
	hasNetworks := false

	// Inherit from ScalityUI Networks if available
	if ui.Spec.Networks != nil {
		hasNetworks = true
		merged.IngressClassName = ui.Spec.Networks.IngressClassName
		merged.Host = ui.Spec.Networks.Host
		merged.TLS = ui.Spec.Networks.TLS
		merged.IngressAnnotations = make(map[string]string)
		for k, v := range ui.Spec.Networks.IngressAnnotations {
			merged.IngressAnnotations[k] = v
		}
	}

	// Set default path from component's publicPath if available
	if component.Status.PublicPath != "" {
		path = component.Status.PublicPath
	} else {
		path = "/" + component.Name // fallback path
	}

	// Override with exposer-specific configuration if provided
	if exposer.Spec.Networks != nil {
		hasNetworks = true
		if exposer.Spec.Networks.IngressClassName != "" {
			merged.IngressClassName = exposer.Spec.Networks.IngressClassName
		}
		if exposer.Spec.Networks.Host != "" {
			merged.Host = exposer.Spec.Networks.Host
		}
		if len(exposer.Spec.Networks.TLS) > 0 {
			merged.TLS = exposer.Spec.Networks.TLS
		}
		if len(exposer.Spec.Networks.IngressAnnotations) > 0 {
			if merged.IngressAnnotations == nil {
				merged.IngressAnnotations = make(map[string]string)
			}
			for k, v := range exposer.Spec.Networks.IngressAnnotations {
				merged.IngressAnnotations[k] = v
			}
		}
	}

	if !hasNetworks {
		return nil, ""
	}

	return merged, path
}

// buildIngress configures the Ingress resource
func (r *ScalityUIComponentExposerReconciler) buildIngress(
	ingress *networkingv1.Ingress,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	component *uiv1alpha1.ScalityUIComponent,
	networks *uiv1alpha1.UINetworks,
	path string,
) error {
	// Set owner reference
	if err := controllerutil.SetControllerReference(exposer, ingress, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Set annotations
	if ingress.Annotations == nil {
		ingress.Annotations = make(map[string]string)
	}
	for k, v := range networks.IngressAnnotations {
		ingress.Annotations[k] = v
	}

	// Configure ingress spec
	// Use Exact path type for precise matching of .well-known paths
	defaultPathType := networkingv1.PathTypeExact

	// Add rewrite annotation for exposer runtime configuration path
	if ingress.Annotations == nil {
		ingress.Annotations = make(map[string]string)
	}

	// Configure different behavior based on access pattern (IP vs Host)
	// For IP access (controlplane): rewrite .well-known requests to specific exposer config
	// For Host access (workloadplane): handle differently based on the access pattern

	// Use configuration-snippet for conditional rewriting
	// Only rewrite the specific runtime-app-configuration path, leave other paths as-is
	configSnippet := fmt.Sprintf(`
if ($request_uri ~ "^%s/\.well-known/runtime-app-configuration") {
    rewrite ^.*$ /.well-known/configs/%s break;
}
`, path, exposer.Name)
	ingress.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = configSnippet

	// Remove rewrite-target annotation since we're using configuration-snippet
	delete(ingress.Annotations, "nginx.ingress.kubernetes.io/rewrite-target")

	// Create paths for both runtime configuration and general micro-app resources
	prefixPathType := networkingv1.PathTypePrefix

	ingressRule := networkingv1.IngressRule{
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{
					// Specific path for runtime configuration with rewrite
					{
						Path:     path + "/.well-known/runtime-app-configuration",
						PathType: &defaultPathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: component.Name, // Service name matches component name
								Port: networkingv1.ServiceBackendPort{
									Number: 80,
								},
							},
						},
					},
					// General path for all other micro-app resources
					{
						Path:     path + "/",
						PathType: &prefixPathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: component.Name, // Service name matches component name
								Port: networkingv1.ServiceBackendPort{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	// Set host if provided
	if networks.Host != "" {
		ingressRule.Host = networks.Host
	}

	ingress.Spec = networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{ingressRule},
	}

	// Set IngressClassName if provided
	if networks.IngressClassName != "" {
		ingress.Spec.IngressClassName = &networks.IngressClassName
	}

	// Set TLS configuration if provided
	if len(networks.TLS) > 0 {
		ingress.Spec.TLS = networks.TLS
	}

	return nil
}

// Note: ensureIngressDeleted function removed since Ingress cleanup is handled
// automatically by Kubernetes garbage collection via Owns(&networkingv1.Ingress{})

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&networkingv1.Ingress{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.findExposersForConfigMap)).
		Watches(&uiv1alpha1.ScalityUI{}, handler.EnqueueRequestsFromMapFunc(r.findExposersForScalityUI)).
		Watches(&uiv1alpha1.ScalityUIComponent{}, handler.EnqueueRequestsFromMapFunc(r.findExposersForScalityUIComponent)).
		Complete(r)
}
