package scalityuicomponentexposer

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newDependencyValidationReducer creates a StateReducer for validating dependencies
func newDependencyValidationReducer() StateReducer {
	return StateReducer{
		F: func(cr ScalityUIComponentExposer, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			// Validate and fetch dependencies
			_, _, err := validateAndFetchDependencies(ctx, cr, currentState, log)
			if err != nil {
				return handleDependencyError(ctx, cr, err, currentState, log)
			}

			// Mark dependencies as ready
			setStatusCondition(cr, conditionTypeDependenciesReady, metav1.ConditionTrue,
				reasonReconcileSucceeded, "All dependencies are available")

			return reconcile.Result{}, nil
		},
		N: "dependency-validation",
	}
}

// validateAndFetchDependencies validates and fetches the required ScalityUIComponent and ScalityUI resources
func validateAndFetchDependencies(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	currentState State,
	logger logr.Logger,
) (*uiv1alpha1.ScalityUIComponent, *uiv1alpha1.ScalityUI, error) {

	client := currentState.GetKubeClient()

	// Fetch ScalityUIComponent
	component := &uiv1alpha1.ScalityUIComponent{}
	componentKey := types.NamespacedName{
		Name:      exposer.Spec.ScalityUIComponent,
		Namespace: exposer.Namespace,
	}

	if err := client.Get(ctx, componentKey, component); err != nil {
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

	if err := client.Get(ctx, uiKey, ui); err != nil {
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
func handleDependencyError(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	err error,
	currentState State,
	logger logr.Logger,
) (reconcile.Result, error) {
	logger.Info("Dependency validation failed", "error", err.Error())

	setStatusCondition(exposer, conditionTypeDependenciesReady, metav1.ConditionFalse,
		reasonDependencyMissing, err.Error())

	if updateErr := updateStatus(ctx, exposer, currentState, logger); updateErr != nil {
		logger.Error(updateErr, "Failed to update status after dependency error")
	}

	// Requeue after a longer interval for missing dependencies
	return reconcile.Result{RequeueAfter: defaultRequeueInterval}, nil
}
