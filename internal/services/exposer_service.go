package services

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ExposerService handles business logic related to ScalityUIComponentExposer resources
type ExposerService struct {
	client client.Client
}

// NewExposerService creates a new ExposerService
func NewExposerService(client client.Client) *ExposerService {
	return &ExposerService{
		client: client,
	}
}

// FindAllExposersForUI finds all ScalityUIComponentExposer resources that reference the given UI
func (s *ExposerService) FindAllExposersForUI(ctx context.Context, uiName string) ([]uiscalitycomv1alpha1.ScalityUIComponentExposer, error) {
	exposerList := &uiscalitycomv1alpha1.ScalityUIComponentExposerList{}
	if err := s.client.List(ctx, exposerList); err != nil {
		return nil, fmt.Errorf("failed to list exposers: %w", err)
	}

	var result []uiscalitycomv1alpha1.ScalityUIComponentExposer
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUI == uiName {
			result = append(result, exposer)
		}
	}

	return result, nil
}

// GetComponentForExposer retrieves the ScalityUIComponent referenced by an exposer
func (s *ExposerService) GetComponentForExposer(ctx context.Context, exposer *uiscalitycomv1alpha1.ScalityUIComponentExposer) (*uiscalitycomv1alpha1.ScalityUIComponent, error) {
	component := &uiscalitycomv1alpha1.ScalityUIComponent{}
	componentKey := types.NamespacedName{
		Name:      exposer.Spec.ScalityUIComponent,
		Namespace: exposer.Namespace,
	}

	if err := s.client.Get(ctx, componentKey, component); err != nil {
		return nil, fmt.Errorf("failed to get component %s in namespace %s: %w",
			exposer.Spec.ScalityUIComponent, exposer.Namespace, err)
	}

	return component, nil
}

// FindUIComponentsByExposer finds all ScalityUIComponent resources referenced by exposers for a given UI
func (s *ExposerService) FindUIComponentsByExposer(ctx context.Context, uiName string, logger logr.Logger) ([]uiscalitycomv1alpha1.ScalityUIComponent, error) {
	exposers, err := s.FindAllExposersForUI(ctx, uiName)
	if err != nil {
		return nil, fmt.Errorf("failed to find exposers for UI %s: %w", uiName, err)
	}

	var components []uiscalitycomv1alpha1.ScalityUIComponent
	for _, exposer := range exposers {
		component, err := s.GetComponentForExposer(ctx, &exposer)
		if err != nil {
			// Log the error with context about which exposer failed
			logger.Error(err, "Failed to get component for exposer, skipping",
				"exposer", exposer.Name,
				"namespace", exposer.Namespace,
				"component", exposer.Spec.ScalityUIComponent,
				"ui", uiName)
			continue
		}
		components = append(components, *component)
	}

	return components, nil
}
