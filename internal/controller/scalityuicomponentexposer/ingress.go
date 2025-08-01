package scalityuicomponentexposer

import (
	"fmt"

	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
)

// newIngressReducer creates a StateReducer for managing the ingress using the framework
func newIngressReducer(r *ScalityUIComponentExposerReconciler) StateReducer {
	return asStateReducer(r, newScalityUIComponentExposerIngressReconciler, "ingress")
}

type scalityUIComponentExposerIngressReconciler struct {
	resources.IngressReconciler[ScalityUIComponentExposer, State]
}

var _ reconciler.ResourceReconciler[*networkingv1.Ingress] = &scalityUIComponentExposerIngressReconciler{}

func newScalityUIComponentExposerIngressReconciler(cr ScalityUIComponentExposer, currentState State) reconciler.ResourceReconciler[*networkingv1.Ingress] {
	ctx := currentState.GetContext()

	// Get dependencies directly
	component, ui, err := validateAndFetchDependencies(ctx, cr, currentState, currentState.GetLog())
	if err != nil {
		// Return a reconciler that will skip processing
		return &scalityUIComponentExposerIngressReconciler{
			IngressReconciler: resources.IngressReconciler[ScalityUIComponentExposer, State]{
				CR:           cr,
				CurrentState: currentState,
				Skip:         true, // Skip if dependencies not found
			},
		}
	}

	// Check if networks configuration is available
	networksConfig, path, err := getNetworksConfig(ui, component)
	if err != nil || networksConfig == nil {
		// Return a reconciler that will skip processing
		return &scalityUIComponentExposerIngressReconciler{
			IngressReconciler: resources.IngressReconciler[ScalityUIComponentExposer, State]{
				CR:           cr,
				CurrentState: currentState,
				Skip:         true, // Skip if networks not configured
			},
		}
	}

	return &scalityUIComponentExposerIngressReconciler{
		IngressReconciler: resources.IngressReconciler[ScalityUIComponentExposer, State]{
			CR:              cr,
			CurrentState:    currentState,
			Skip:            false, // Don't skip - we have networks config
			ComponentClass:  "",
			ServiceName:     component.Name, // Service name matches component name
			SubresourceType: "ingress",
			IngressClass:    getIngressClassName(networksConfig),
			Rules: func() ([]resources.IngressHostPath, error) {
				return getIngressRules(networksConfig, path), nil
			},
			AutoGenerateTLS: false, // We'll handle TLS manually if needed
			SkipTLS:         shouldSkipTLS(networksConfig),
			ComputeName: func(subresourceType string, componentClass string) string {
				return cr.Name // Use exposer name as ingress name
			},
			Annotations: getIngressAnnotations(networksConfig, path, cr.Name),
		},
	}
}

// getNetworksConfig inherits networks configuration from ScalityUI
func getNetworksConfig(
	ui *uiv1alpha1.ScalityUI,
	component *uiv1alpha1.ScalityUIComponent,
) (*uiv1alpha1.UINetworks, string, error) {
	// PublicPath is mandatory for each micro app
	if component.Status.PublicPath == "" {
		return nil, "", fmt.Errorf("component %s does not have PublicPath set in status - this is required for ingress configuration", component.Name)
	}

	path := component.Status.PublicPath

	// Inherit from ScalityUI Networks if available
	if ui.Spec.Networks != nil {
		return ui.Spec.Networks, path, nil
	}

	return nil, "", nil
}

// getIngressClassName returns the ingress class name from networks config
func getIngressClassName(networks *uiv1alpha1.UINetworks) string {
	return networks.IngressClassName
}

// getIngressRules returns the ingress rules based on networks config
func getIngressRules(networks *uiv1alpha1.UINetworks, path string) []resources.IngressHostPath {
	rules := []resources.IngressHostPath{
		{
			Host: networks.Host,
			Path: path + "/",
		},
	}
	return rules
}

// shouldSkipTLS determines if TLS should be skipped
func shouldSkipTLS(networks *uiv1alpha1.UINetworks) bool {
	return len(networks.TLS) == 0
}

// getIngressAnnotations returns the ingress annotations with rewrite rules
func getIngressAnnotations(networks *uiv1alpha1.UINetworks, path string, exposerName string) map[string]string {
	annotations := make(map[string]string)

	// Copy annotations from networks config
	for k, v := range networks.IngressAnnotations {
		annotations[k] = v
	}

	// Add rewrite annotation for exposer runtime configuration path
	// Use configuration-snippet for conditional rewriting
	configSnippet := fmt.Sprintf(`
if ($request_uri ~ "^%s/\\.well-known/runtime-app-configuration(\\?.*)?$") {
    rewrite ^.*$ /.well-known/%s/%s break;
}
`, path, configsSubdirectory, exposerName)
	annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = configSnippet

	return annotations
}

// NewReferenceResource overrides the framework's method to handle custom TLS configuration and correct service names
func (r *scalityUIComponentExposerIngressReconciler) NewReferenceResource() (*networkingv1.Ingress, error) {
	// Call the parent method to get the ingress
	ingress, err := r.IngressReconciler.NewReferenceResource()
	if err != nil {
		return nil, err
	}

	if ingress == nil {
		return nil, nil
	}

	// Get dependencies to configure TLS
	ctx := r.CurrentState.GetContext()
	_, ui, err := validateAndFetchDependencies(ctx, r.CR, r.CurrentState, r.CurrentState.GetLog())
	if err != nil {
		return ingress, nil
	}

	// Set TLS configuration if provided in networks config
	if ui.Spec.Networks != nil && len(ui.Spec.Networks.TLS) > 0 {
		ingress.Spec.TLS = ui.Spec.Networks.TLS
	}

	for i := range ingress.Spec.Rules {
		rule := &ingress.Spec.Rules[i]
		if rule.HTTP != nil {
			for j := range rule.HTTP.Paths {
				path := &rule.HTTP.Paths[j]
				if path.Backend.Service != nil {
					path.Backend.Service.Name = r.ServiceName
				}
			}
		}
	}

	return ingress, nil
}

// Labels returns the labels for the ingress resource
func (r *scalityUIComponentExposerIngressReconciler) Labels() map[string]string {
	return map[string]string{
		"app":                          r.CR.Name,
		"app.kubernetes.io/name":       r.CR.Name,
		"app.kubernetes.io/component":  "ingress",
		"app.kubernetes.io/part-of":    "ui-operator",
		"app.kubernetes.io/managed-by": "ui-operator",
		"ui.scality.com/exposer":       r.CR.Name,
		"ui.scality.com/component":     r.CR.Spec.ScalityUIComponent,
		"ui.scality.com/ui":            r.CR.Spec.ScalityUI,
	}
}
