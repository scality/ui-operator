package scalityuicomponent

import (
	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// newServiceReducer creates a StateReducer for managing the service using the framework
func newServiceReducer(r *ScalityUIComponentReconciler) StateReducer {
	return asStateReducer(r, newScalityUIComponentServiceReconciler, "service")
}

type scalityUIComponentServiceReconciler struct {
	resources.ServiceReconciler[ScalityUIComponent, State]
}

var _ reconciler.ResourceReconciler[*corev1.Service] = &scalityUIComponentServiceReconciler{}

func newScalityUIComponentServiceReconciler(cr ScalityUIComponent, currentState State) reconciler.ResourceReconciler[*corev1.Service] {
	return &scalityUIComponentServiceReconciler{
		ServiceReconciler: resources.ServiceReconciler[ScalityUIComponent, State]{
			ResourceReconciler: resources.ResourceReconciler[ScalityUIComponent, State, *corev1.Service]{
				CR:              cr,
				CurrentState:    currentState,
				SubresourceType: "service",
			},
			PortName:   "http",
			PortNum:    DefaultServicePort,
			TargetPort: intstr.FromInt(DefaultServicePort),
			Selectors: map[string]string{
				"app": cr.Name,
			},
		},
	}
}
