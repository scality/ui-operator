package mappers

import (
	"context"

	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// UIEventMapper handles mapping events from related resources to ScalityUI reconcile requests
type UIEventMapper struct {
	client client.Client
}

// NewUIEventMapper creates a new UIEventMapper
func NewUIEventMapper(client client.Client) *UIEventMapper {
	return &UIEventMapper{
		client: client,
	}
}

// MapExposerToUI maps ScalityUIComponentExposer events to ScalityUI reconcile requests
func (m *UIEventMapper) MapExposerToUI(ctx context.Context, obj client.Object) []reconcile.Request {
	exposer, ok := obj.(*uiscalitycomv1alpha1.ScalityUIComponentExposer)
	if !ok {
		return nil
	}

	// Return a reconcile request for the referenced ScalityUI
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name: exposer.Spec.ScalityUI,
			},
		},
	}
}

// MapComponentToUI maps ScalityUIComponent events to ScalityUI reconcile requests
func (m *UIEventMapper) MapComponentToUI(ctx context.Context, obj client.Object) []reconcile.Request {
	component, ok := obj.(*uiscalitycomv1alpha1.ScalityUIComponent)
	if !ok {
		return nil
	}

	// Find all exposers that reference this component
	exposerList := &uiscalitycomv1alpha1.ScalityUIComponentExposerList{}
	if err := m.client.List(ctx, exposerList, client.InNamespace(component.Namespace)); err != nil {
		return nil
	}

	// Collect unique UI names that should be reconciled
	uiNames := make(map[string]bool)
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUIComponent == component.Name {
			uiNames[exposer.Spec.ScalityUI] = true
		}
	}

	// Convert to reconcile requests
	var requests []reconcile.Request
	for uiName := range uiNames {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: uiName,
			},
		})
	}

	return requests
}
