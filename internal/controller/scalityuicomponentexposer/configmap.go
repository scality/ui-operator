package scalityuicomponentexposer

import (
	"encoding/json"
	"fmt"

	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"github.com/scality/ui-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// newConfigMapReducer creates a StateReducer for managing ConfigMaps using the framework
func newConfigMapReducer(r *ScalityUIComponentExposerReconciler) StateReducer {
	return asStateReducer(r, newScalityUIComponentExposerConfigMapReconciler, "configmap")
}

// scalityUIComponentExposerConfigMapReconciler implements the framework's ResourceReconciler for ConfigMaps
type scalityUIComponentExposerConfigMapReconciler struct {
	resources.ResourceReconciler[ScalityUIComponentExposer, State, *corev1.ConfigMap]
	component *uiv1alpha1.ScalityUIComponent
	ui        *uiv1alpha1.ScalityUI
}

var _ reconciler.ResourceReconciler[*corev1.ConfigMap] = &scalityUIComponentExposerConfigMapReconciler{}

func newScalityUIComponentExposerConfigMapReconciler(cr ScalityUIComponentExposer, currentState State) reconciler.ResourceReconciler[*corev1.ConfigMap] {
	ctx := currentState.GetContext()

	// Get dependencies directly
	component, ui, err := validateAndFetchDependencies(ctx, cr, currentState, currentState.GetLog())
	if err != nil {
		// Return a reconciler that will skip processing
		return &scalityUIComponentExposerConfigMapReconciler{
			ResourceReconciler: resources.ResourceReconciler[ScalityUIComponentExposer, State, *corev1.ConfigMap]{
				CR:              cr,
				CurrentState:    currentState,
				SubresourceType: "configmap",
			},
			// component and ui will be nil, indicating skip
		}
	}

	return &scalityUIComponentExposerConfigMapReconciler{
		ResourceReconciler: resources.ResourceReconciler[ScalityUIComponentExposer, State, *corev1.ConfigMap]{
			CR:              cr,
			CurrentState:    currentState,
			SubresourceType: "configmap",
		},
		component: component,
		ui:        ui,
	}
}

// NewZeroResource returns a new zero-value ConfigMap
func (r *scalityUIComponentExposerConfigMapReconciler) NewZeroResource() *corev1.ConfigMap {
	return &corev1.ConfigMap{}
}

// NewReferenceResource creates the desired ConfigMap state
func (r *scalityUIComponentExposerConfigMapReconciler) NewReferenceResource() (*corev1.ConfigMap, error) {
	// Skip if dependencies not available
	if r.component == nil || r.ui == nil {
		return nil, nil
	}

	// Use component name for ConfigMap name with suffix
	configMapName := fmt.Sprintf("%s-%s", r.component.Name, configMapNameSuffix)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: r.CR.GetNamespace(),
			Labels:    r.getLabels(),
		},
	}

	return configMap, nil
}

// PreCreate is called before creating the ConfigMap
func (r *scalityUIComponentExposerConfigMapReconciler) PreCreate(configMap *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	return r.prepareConfigMap(configMap)
}

// PreUpdate is called before updating the ConfigMap
func (r *scalityUIComponentExposerConfigMapReconciler) PreUpdate(configMap *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	return r.prepareConfigMap(configMap)
}

// prepareConfigMap applies all the business logic to the ConfigMap
func (r *scalityUIComponentExposerConfigMapReconciler) prepareConfigMap(configMap *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	// Skip if dependencies not available
	if r.component == nil || r.ui == nil {
		return configMap, nil
	}

	// Add exposer-specific finalizer to the ConfigMap so it is only deleted when all exposers are gone
	finalizerName := configMapFinalizerPrefix + r.CR.GetName()
	if !controllerutil.ContainsFinalizer(configMap, finalizerName) {
		controllerutil.AddFinalizer(configMap, finalizerName)
	}

	// Build and add the runtime configuration for this exposer
	if err := r.updateConfigMapData(configMap); err != nil {
		return nil, fmt.Errorf("failed to update ConfigMap data: %w", err)
	}

	return configMap, nil
}

// updateConfigMapData updates the ConfigMap data with the runtime configuration
func (r *scalityUIComponentExposerConfigMapReconciler) updateConfigMapData(configMap *corev1.ConfigMap) error {
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}

	runtimeConfig, err := r.buildRuntimeConfiguration()
	if err != nil {
		return fmt.Errorf("failed to build runtime configuration: %w", err)
	}

	configJSONBytes, err := json.MarshalIndent(runtimeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal runtime configuration: %w", err)
	}

	// Use exposer name as the key in ConfigMap
	configMap.Data[r.CR.GetName()] = string(configJSONBytes)
	return nil
}

// buildRuntimeConfiguration builds the MicroAppRuntimeConfiguration struct
func (r *scalityUIComponentExposerConfigMapReconciler) buildRuntimeConfiguration() (MicroAppRuntimeConfiguration, error) {
	authConfig, err := utils.BuildAuthConfig(r.CR.Spec.Auth)
	if err != nil {
		return MicroAppRuntimeConfiguration{}, fmt.Errorf("failed to build auth config: %w", err)
	}

	// Parse SelfConfiguration from RawExtension
	var selfConfig map[string]interface{}
	if r.CR.Spec.SelfConfiguration != nil && r.CR.Spec.SelfConfiguration.Raw != nil {
		if err := json.Unmarshal(r.CR.Spec.SelfConfiguration.Raw, &selfConfig); err != nil {
			return MicroAppRuntimeConfiguration{}, fmt.Errorf("failed to unmarshal self configuration: %w", err)
		}
	}

	return MicroAppRuntimeConfiguration{
		Kind:       runtimeConfigKind,
		APIVersion: runtimeConfigAPIVersion,
		Metadata: MicroAppRuntimeConfigurationMetadata{
			Kind: r.component.Status.Kind,
			Name: r.component.Name,
		},
		Spec: MicroAppRuntimeConfigurationSpec{
			ScalityUI:          r.ui.Name,
			ScalityUIComponent: r.component.Name,
			AppHistoryBasePath: r.CR.Spec.AppHistoryBasePath,
			Auth:               authConfig,
			SelfConfiguration:  selfConfig,
		},
	}, nil
}

// getLabels returns labels for the ConfigMap
func (r *scalityUIComponentExposerConfigMapReconciler) getLabels() map[string]string {
	return map[string]string{
		"app":                          r.CR.GetName(),
		"app.kubernetes.io/name":       r.CR.GetName(),
		"app.kubernetes.io/component":  "configmap",
		"app.kubernetes.io/part-of":    "ui-operator",
		"app.kubernetes.io/managed-by": "ui-operator",
		"ui.scality.com/exposer":       r.CR.GetName(),
		"ui.scality.com/component":     r.CR.Spec.ScalityUIComponent,
		"ui.scality.com/ui":            r.CR.Spec.ScalityUI,
	}
}

// Dependencies returns the list of dependencies for this reconciler
func (r *scalityUIComponentExposerConfigMapReconciler) Dependencies() []string {
	return []string{} // Dependencies are handled in the constructor
}

// AsHashable returns a hashable representation for comparison
func (r *scalityUIComponentExposerConfigMapReconciler) AsHashable(configMap *corev1.ConfigMap) interface{} {
	if configMap == nil {
		return nil
	}

	return map[string]interface{}{
		"name":      configMap.Name,
		"namespace": configMap.Namespace,
		"data":      configMap.Data,
		"labels":    configMap.Labels,
	}
}

// IsNil checks if the ConfigMap is nil
func (r *scalityUIComponentExposerConfigMapReconciler) IsNil(configMap *corev1.ConfigMap) bool {
	return configMap == nil
}

// PostReconcile is called after successful reconciliation
func (r *scalityUIComponentExposerConfigMapReconciler) PostReconcile(configMap *corev1.ConfigMap) {
	// Set status condition for successful ConfigMap reconciliation
	setStatusCondition(r.CR, conditionTypeConfigMapReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "ConfigMap successfully created/updated")
}

// Validate performs validation
func (r *scalityUIComponentExposerConfigMapReconciler) Validate() error {
	// Skip validation if dependencies not available
	if r.component == nil || r.ui == nil {
		return fmt.Errorf("dependencies not available")
	}

	// Validate auth configuration if present
	if r.CR.Spec.Auth != nil {
		if err := utils.ValidateAuthConfig(r.CR.Spec.Auth); err != nil {
			return fmt.Errorf("invalid auth configuration: %w", err)
		}
	}

	return nil
}
