package scalityuicomponentexposer

import (
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"github.com/scality/ui-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// newConfigMapReducer creates a StateReducer for ConfigMap reconciliation
func newConfigMapReducer(r *ScalityUIComponentExposerReconciler) StateReducer {
	return StateReducer{
		N: "configmap",
		F: func(cr ScalityUIComponentExposer, state State, log logr.Logger) (ctrl.Result, error) {
			return r.reconcileConfigMap(cr, state, log)
		},
	}
}

// reconcileConfigMap creates or updates the ConfigMap for a ScalityUIComponentExposer
func (r *ScalityUIComponentExposerReconciler) reconcileConfigMap(
	cr ScalityUIComponentExposer,
	state State,
	log logr.Logger,
) (ctrl.Result, error) {
	ctx := state.GetContext()

	// Get dependencies
	component, ui, err := validateAndFetchDependencies(ctx, cr, state, log)
	if err != nil {
		log.Info("Skipping ConfigMap reconciliation due to missing dependencies", "error", err.Error())
		return ctrl.Result{}, nil
	}

	configMapName := fmt.Sprintf("%s-%s", component.Name, configMapNameSuffix)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: cr.GetNamespace(),
		},
	}

	log.Info("Reconciling ConfigMap", "configMap", configMapName)

	result, err := ctrl.CreateOrUpdate(ctx, state.GetKubeClient(), configMap, func() error {
		// Add exposer-specific finalizer to the ConfigMap so it is only deleted when all exposers are gone
		finalizerName := configMapFinalizerPrefix + cr.GetName()
		if !controllerutil.ContainsFinalizer(configMap, finalizerName) {
			controllerutil.AddFinalizer(configMap, finalizerName)
		}

		// Build and add the runtime configuration for this exposer
		if err := r.updateConfigMapData(configMap, cr, ui, component); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}

	log.Info("Successfully reconciled ConfigMap",
		"configMap", configMapName, "operation", result)

	// Set status condition
	setStatusCondition(cr, conditionTypeConfigMapReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "ConfigMap successfully created/updated")

	return ctrl.Result{}, nil
}

// updateConfigMapData updates the ConfigMap data with the runtime configuration
func (r *ScalityUIComponentExposerReconciler) updateConfigMapData(
	configMap *corev1.ConfigMap,
	exposer ScalityUIComponentExposer,
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

	configMap.Data[exposer.GetName()] = string(configJSONBytes)
	return nil
}

// buildRuntimeConfiguration builds the MicroAppRuntimeConfiguration struct
func (r *ScalityUIComponentExposerReconciler) buildRuntimeConfiguration(
	exposer ScalityUIComponentExposer,
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
) (MicroAppRuntimeConfiguration, error) {
	authConfig, err := utils.BuildAuthConfig(ui.Spec.Auth)
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
			Auth:               authConfig,
			SelfConfiguration:  selfConfig,
			AppHistoryBasePath: exposer.Spec.AppHistoryBasePath,
		},
	}, nil
}
