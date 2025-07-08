package scalityui

import (
	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	networkingv1 "k8s.io/api/networking/v1"
)

// newIngressReducer creates a StateReducer for managing the ingress using the framework
func newIngressReducer(r *ScalityUIReconciler) StateReducer {
	return asStateReducer(r, newScalityUIIngressReconciler, "ingress")
}

type scalityUIIngressReconciler struct {
	resources.IngressReconciler[ScalityUI, State]
}

var _ reconciler.ResourceReconciler[*networkingv1.Ingress] = &scalityUIIngressReconciler{}

func newScalityUIIngressReconciler(cr ScalityUI, currentState State) reconciler.ResourceReconciler[*networkingv1.Ingress] {
	return &scalityUIIngressReconciler{
		IngressReconciler: resources.IngressReconciler[ScalityUI, State]{
			CR:              cr,
			CurrentState:    currentState,
			Skip:            false, // Always create ingress
			ComponentClass:  "",
			ServiceName:     "service", // This will be combined with CR name to form the service name
			ServicePort:     "http",
			SubresourceType: "ingress",
			IngressClass:    getIngressClass(cr),
			Rules: func() ([]resources.IngressHostPath, error) {
				return getIngressRules(cr), nil
			},
			AutoGenerateTLS: false, // We'll handle TLS manually if needed
			SkipTLS:         shouldSkipTLS(cr),
			ComputeName: func(subresourceType string, componentClass string) string {
				return subresourceType
			},
			Annotations: getIngressAnnotations(cr),
		},
	}
}

// NewReferenceResource overrides the framework's method to handle namespace for cluster-scoped resources
func (r *scalityUIIngressReconciler) NewReferenceResource() (*networkingv1.Ingress, error) {
	// Call the parent method to get the ingress
	ingress, err := r.IngressReconciler.NewReferenceResource()
	if err != nil {
		return nil, err
	}

	if ingress == nil {
		return nil, nil
	}

	// Override the namespace for cluster-scoped resources
	ingress.Namespace = getOperatorNamespace()

	return ingress, nil
}

// Helper functions for ingress configuration
func getIngressClass(cr ScalityUI) string {
	if cr.Spec.Networks != nil && cr.Spec.Networks.IngressClassName != "" {
		return cr.Spec.Networks.IngressClassName
	}
	return ""
}

func getIngressRules(cr ScalityUI) []resources.IngressHostPath {
	rules := []resources.IngressHostPath{
		{
			Host: getHostname(cr),
			Path: "/",
		},
	}
	return rules
}

func getHostname(cr ScalityUI) string {
	if cr.Spec.Networks != nil && cr.Spec.Networks.Host != "" {
		return cr.Spec.Networks.Host
	}
	return "" // Default to no host (matches all)
}

func shouldSkipTLS(cr ScalityUI) bool {
	if cr.Spec.Networks != nil && len(cr.Spec.Networks.TLS) > 0 {
		return false // Don't skip TLS if configured
	}
	return true // Skip TLS by default
}

func getIngressAnnotations(cr ScalityUI) map[string]string {
	annotations := make(map[string]string)

	if cr.Spec.Networks != nil && len(cr.Spec.Networks.IngressAnnotations) > 0 {
		for key, value := range cr.Spec.Networks.IngressAnnotations {
			annotations[key] = value
		}
	}

	return annotations
}
