package scalityuicomponentexposer

import (
	"context"
	"strings"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// findExposersForScalityUI finds all ScalityUIComponentExposer resources that reference a given ScalityUI
func (r *ScalityUIComponentExposerReconciler) findExposersForScalityUI(ctx context.Context, obj client.Object) []reconcile.Request {
	scalityUI, ok := obj.(*uiv1alpha1.ScalityUI)
	if !ok {
		return nil
	}

	// List all ScalityUIComponentExposer resources
	exposerList := &uiv1alpha1.ScalityUIComponentExposerList{}
	if err := r.Client.List(ctx, exposerList); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUI == scalityUI.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&exposer),
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
	if err := r.Client.List(ctx, exposerList, client.InNamespace(component.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUIComponent == component.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&exposer),
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

	// We expect ConfigMap name format: <component-name>-runtime-app-configuration
	suffix := "-" + configMapNameSuffix
	if !strings.HasSuffix(cm.Name, suffix) {
		return nil
	}

	componentName := strings.TrimSuffix(cm.Name, suffix)

	// List all exposers in the same namespace that reference this component
	exposerList := &uiv1alpha1.ScalityUIComponentExposerList{}
	if err := r.Client.List(ctx, exposerList, client.InNamespace(cm.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, exposer := range exposerList.Items {
		if exposer.Spec.ScalityUIComponent == componentName {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&exposer),
			})
		}
	}
	return requests
}
