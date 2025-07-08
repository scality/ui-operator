package scalityui

import (
	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// newServiceReducer creates a StateReducer for managing the service using the framework
func newServiceReducer(r *ScalityUIReconciler) StateReducer {
	return asStateReducer(r, newScalityUIServiceReconciler, "service")
}

type scalityUIServiceReconciler struct {
	resources.ServiceReconciler[ScalityUI, State]
}

var _ reconciler.ResourceReconciler[*corev1.Service] = &scalityUIServiceReconciler{}

func newScalityUIServiceReconciler(cr ScalityUI, currentState State) reconciler.ResourceReconciler[*corev1.Service] {
	return &scalityUIServiceReconciler{
		ServiceReconciler: resources.ServiceReconciler[ScalityUI, State]{
			ResourceReconciler: resources.ResourceReconciler[ScalityUI, State, *corev1.Service]{
				CR:              cr,
				CurrentState:    currentState,
				SubresourceType: "service",
				Dependencies: func() []string {
					return []string{} // No dependencies for now
				},
				Labels: func() map[string]string {
					return map[string]string{
						"app": cr.Name,
					}
				},
			},
			PortName:   "http",
			PortNum:    uiServicePort,
			TargetPort: intstr.FromInt(uiServicePort),
			Selectors: map[string]string{
				"app": cr.Name,
			},
		},
	}
}

// NewReferenceResource overrides the framework's method to handle namespace for cluster-scoped resources
func (r *scalityUIServiceReconciler) NewReferenceResource() (*corev1.Service, error) {
	// Call the parent method to get the service
	service, err := r.ServiceReconciler.NewReferenceResource()
	if err != nil {
		return nil, err
	}

	if service == nil {
		return nil, nil
	}

	// Override the namespace for cluster-scoped resources
	service.Namespace = getOperatorNamespace()

	return service, nil
}
