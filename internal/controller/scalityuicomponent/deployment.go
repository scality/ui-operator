package scalityuicomponent

import (
	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	"github.com/scality/ui-operator/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// newDeploymentReducer creates a StateReducer for managing the deployment using the framework
func newDeploymentReducer(r *ScalityUIComponentReconciler) StateReducer {
	return asStateReducer(r, newScalityUIComponentDeploymentReconciler, "deployment")
}

type scalityUIComponentDeploymentReconciler struct {
	resources.DeploymentReconciler[ScalityUIComponent, State]
}

var _ reconciler.ResourceReconciler[*appsv1.Deployment] = &scalityUIComponentDeploymentReconciler{}

func newScalityUIComponentDeploymentReconciler(cr ScalityUIComponent, currentState State) reconciler.ResourceReconciler[*appsv1.Deployment] {
	return &scalityUIComponentDeploymentReconciler{
		DeploymentReconciler: resources.DeploymentReconciler[ScalityUIComponent, State]{
			WorkloadReconciler: resources.WorkloadReconciler[ScalityUIComponent, State, *appsv1.Deployment]{
				ResourceReconciler: resources.ResourceReconciler[ScalityUIComponent, State, *appsv1.Deployment]{
					CR:              cr,
					CurrentState:    currentState,
					SubresourceType: "deployment",
					Dependencies: func() []string {
						return []string{} // No dependencies for now
					},
					Labels: func() map[string]string {
						return map[string]string{
							"app": cr.Name, // Keep existing label for compatibility
						}
					},
				},
				GetReplicas: func() *int32 {
					return ptr.To(int32(1)) // Default to 1 replica
				},
				Containers: []resources.GenericContainer{
					{
						Name:  cr.Name,
						Image: utils.ParseImageName(cr.Spec.Image),
						Tag:   utils.ParseImageTag(cr.Spec.Image),
						Ports: []corev1.ContainerPort{},
						Env: func() ([]corev1.EnvVar, error) {
							return []corev1.EnvVar{}, nil
						},
						VolumeMounts: func() ([]corev1.VolumeMount, error) {
							return []corev1.VolumeMount{}, nil
						},
					},
				},
				Volumes: func() ([]corev1.Volume, error) {
					return []corev1.Volume{}, nil
				},
			},
		},
	}
}
