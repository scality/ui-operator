package scalityui

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	"github.com/scality/ui-operator/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// newDeploymentReducer creates a StateReducer for managing the deployment using the framework
func newDeploymentReducer(r *ScalityUIReconciler) StateReducer {
	return asStateReducer(r, newScalityUIDeploymentReconciler, "deployment")
}

type scalityUIDeploymentReconciler struct {
	resources.DeploymentReconciler[ScalityUI, State]
}

var _ reconciler.ResourceReconciler[*appsv1.Deployment] = &scalityUIDeploymentReconciler{}

func newScalityUIDeploymentReconciler(cr ScalityUI, currentState State) reconciler.ResourceReconciler[*appsv1.Deployment] {
	reconciler := &scalityUIDeploymentReconciler{
		DeploymentReconciler: resources.DeploymentReconciler[ScalityUI, State]{
			WorkloadReconciler: resources.WorkloadReconciler[ScalityUI, State, *appsv1.Deployment]{
				ResourceReconciler: resources.ResourceReconciler[ScalityUI, State, *appsv1.Deployment]{
					CR:              cr,
					CurrentState:    currentState,
					SubresourceType: "deployment",
					Dependencies: func() []string {
						return []string{} // No dependencies for now
					},
					Labels: func() map[string]string {
						return map[string]string{
							"app": cr.Name,
						}
					},
				},
				GetReplicas: func() *int32 {
					// Calculate replicas based on available nodes for high availability
					replicas := calculateReplicas(currentState)
					return ptr.To(int32(replicas))
				},
				GetScheduling: func() resources.SchedulingSpec {
					// Add anti-affinity for high availability when we have multiple replicas
					replicas := calculateReplicas(currentState)

					spec := resources.SchedulingSpec{}
					if replicas > 1 {
						spec.Affinity = &corev1.Affinity{
							PodAntiAffinity: &corev1.PodAntiAffinity{
								PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
									{
										Weight: 100,
										PodAffinityTerm: corev1.PodAffinityTerm{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"app": cr.Name,
												},
											},
											TopologyKey: "kubernetes.io/hostname",
										},
									},
								},
							},
						}
					}

					// Add tolerations from CR spec
					if cr.Spec.Scheduling != nil && len(cr.Spec.Scheduling.Tolerations) > 0 {
						spec.Tolerations = cr.Spec.Scheduling.Tolerations
					}

					return spec
				},
				Containers: []resources.GenericContainer{
					{
						Name:  "ui",
						Image: utils.ParseImageName(cr.Spec.Image),
						Tag:   utils.ParseImageTag(cr.Spec.Image),
						Ports: []corev1.ContainerPort{
							{
								Name:          "http",
								ContainerPort: uiServicePort,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						ReadinessProbe: resources.Probe{
							HTTPPort: uiServicePort,
							HTTPPath: "/",
						},
						LivenessProbe: resources.Probe{
							HTTPPort: uiServicePort,
							HTTPPath: "/",
						},
						Env: func() ([]corev1.EnvVar, error) {
							env := []corev1.EnvVar{
								{
									Name:  "NODE_ENV",
									Value: "production",
								},
							}
							return env, nil
						},
						VolumeMounts: func() ([]corev1.VolumeMount, error) {
							return []corev1.VolumeMount{
								{
									Name:      cr.Name + "-config-volume",
									MountPath: "/usr/share/nginx/html/shell/config.json",
									SubPath:   "config.json",
									ReadOnly:  true,
								},
								{
									Name:      cr.Name + "-deployed-ui-apps-volume",
									MountPath: "/usr/share/nginx/html/shell/deployed-ui-apps.json",
									SubPath:   "deployed-ui-apps.json",
									ReadOnly:  true,
								},
							}, nil
						},
					},
				},
				Volumes: func() ([]corev1.Volume, error) {
					return []corev1.Volume{
						{
							Name: cr.Name + "-config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: cr.Name,
									},
								},
							},
						},
						{
							Name: cr.Name + "-deployed-ui-apps-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: cr.Name + "-deployed-ui-apps",
									},
								},
							},
						},
					}, nil
				},
				Options: []resources.WorkloadOption{
					func(podTemplate *corev1.PodTemplateSpec) {
						// Set image pull secrets
						podTemplate.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets

						// Add config hash annotation for rolling updates
						if podTemplate.Annotations == nil {
							podTemplate.Annotations = make(map[string]string)
						}

						// Get config hash from memory (set by configmap reducer)
						// This avoids cache sync issues when reading from Kubernetes API
						configHash, _ := currentState.GetSubresourceHash(configMapHashKey)

						// Get deployed-apps hash from memory (set by deployed-apps-configmap reducer)
						deployedAppsHash, _ := currentState.GetSubresourceHash(deployedAppsConfigMapHashKey)

						// Combine both hashes to trigger rolling update when either changes
						if configHash != "" || deployedAppsHash != "" {
							combined := "config:" + configHash + ";apps:" + deployedAppsHash + ";"
							sum := sha256.Sum256([]byte(combined))
							podTemplate.Annotations["scality.com/combined-hash"] = hex.EncodeToString(sum[:])
						}
					},
				},
			},
		},
	}
	return reconciler
}

// NewReferenceResource overrides the framework's method to handle namespace for cluster-scoped resources
func (r *scalityUIDeploymentReconciler) NewReferenceResource() (*appsv1.Deployment, error) {
	// Call the parent method to get the deployment
	deployment, err := r.DeploymentReconciler.NewReferenceResource()
	if err != nil {
		return nil, err
	}

	if deployment == nil {
		return nil, nil
	}

	// Override the namespace for cluster-scoped resources
	deployment.Namespace = getOperatorNamespace()
	deployment.Spec.Template.Namespace = getOperatorNamespace()

	return deployment, nil
}

// calculateReplicas determines the number of replicas based on available nodes
func calculateReplicas(currentState State) int32 {
	ctx := currentState.GetContext()
	nodeList := &corev1.NodeList{}

	if err := currentState.GetKubeClient().List(ctx, nodeList); err != nil {
		// If we can't get nodes, default to 1 replica
		return 1
	}

	// Count ready nodes
	readyNodes := 0
	for _, node := range nodeList.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				readyNodes++
				break
			}
		}
	}

	// For high availability, use 2 replicas if we have 2 or more nodes
	if readyNodes >= 2 {
		return 2
	}

	// Single node or no nodes, use 1 replica
	return 1
}
