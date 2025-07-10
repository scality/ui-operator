package scalityuicomponentexposer

import (
	"crypto/sha256"
	"fmt"

	"github.com/scality/reconciler-framework/reconciler"
	"github.com/scality/reconciler-framework/resources"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newDeploymentUpdateReducer creates a StateReducer for updating existing deployments using the framework
func newDeploymentUpdateReducer(r *ScalityUIComponentExposerReconciler) StateReducer {
	return asStateReducer(r, newScalityUIComponentExposerDeploymentReconciler, "deployment-update")
}

// scalityUIComponentExposerDeploymentReconciler implements the framework's ResourceReconciler for Deployment updates
type scalityUIComponentExposerDeploymentReconciler struct {
	resources.ResourceReconciler[ScalityUIComponentExposer, State, *appsv1.Deployment]
	component  *uiv1alpha1.ScalityUIComponent
	ui         *uiv1alpha1.ScalityUI
	configHash string
	shouldSkip bool
}

var _ reconciler.ResourceReconciler[*appsv1.Deployment] = &scalityUIComponentExposerDeploymentReconciler{}

func newScalityUIComponentExposerDeploymentReconciler(cr ScalityUIComponentExposer, currentState State) reconciler.ResourceReconciler[*appsv1.Deployment] {
	ctx := currentState.GetContext()
	log := currentState.GetLog()

	// Get dependencies directly
	component, ui, err := validateAndFetchDependencies(ctx, cr, currentState, log)
	if err != nil {
		// Return a reconciler that will skip processing
		return &scalityUIComponentExposerDeploymentReconciler{
			ResourceReconciler: resources.ResourceReconciler[ScalityUIComponentExposer, State, *appsv1.Deployment]{
				CR:              cr,
				CurrentState:    currentState,
				SubresourceType: "deployment-update",
			},
			shouldSkip: true,
		}
	}

	// Calculate config hash for triggering rolling updates
	configHash := calculateConfigHash(cr, ui, component)

	return &scalityUIComponentExposerDeploymentReconciler{
		ResourceReconciler: resources.ResourceReconciler[ScalityUIComponentExposer, State, *appsv1.Deployment]{
			CR:              cr,
			CurrentState:    currentState,
			SubresourceType: "deployment-update",
		},
		component:  component,
		ui:         ui,
		configHash: configHash,
		shouldSkip: false,
	}
}

// NewZeroResource returns a new zero-value Deployment
func (r *scalityUIComponentExposerDeploymentReconciler) NewZeroResource() *appsv1.Deployment {
	return &appsv1.Deployment{}
}

// NewReferenceResource creates the desired Deployment state (but we're updating existing ones)
func (r *scalityUIComponentExposerDeploymentReconciler) NewReferenceResource() (*appsv1.Deployment, error) {
	// Skip if dependencies not available
	if r.shouldSkip || r.component == nil {
		return nil, nil
	}

	// We don't create new deployments, we update existing ones
	// Return nil to indicate we'll handle this in PreUpdate
	return nil, nil
}

// PreCreate is called before creating the Deployment (not used in our case)
func (r *scalityUIComponentExposerDeploymentReconciler) PreCreate(deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	// We don't create deployments, only update existing ones
	return deployment, nil
}

// PreUpdate is called before updating the Deployment
func (r *scalityUIComponentExposerDeploymentReconciler) PreUpdate(deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	// Skip if dependencies not available
	if r.shouldSkip || r.component == nil {
		return deployment, nil
	}

	// Update the deployment with ConfigMap mount and trigger rolling update
	return r.updateDeploymentWithConfigMap(deployment)
}

// updateDeploymentWithConfigMap adds ConfigMap volume and volume mount to the deployment
func (r *scalityUIComponentExposerDeploymentReconciler) updateDeploymentWithConfigMap(deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	configMapName := fmt.Sprintf("%s-%s", r.component.Name, configMapNameSuffix)
	volumeName := volumeNamePrefix + r.component.Name
	mountPath := r.component.Spec.MountPath + "/" + configsSubdirectory

	// Ensure ConfigMap volume exists
	ensureConfigMapVolume(deployment, volumeName, configMapName)

	// Ensure all containers have the volume mount
	for i := range deployment.Spec.Template.Spec.Containers {
		ensureConfigMapVolumeMount(&deployment.Spec.Template.Spec.Containers[i], volumeName, mountPath)
	}

	// Update annotation to trigger rolling update if config changed
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	currentHash := deployment.Spec.Template.Annotations[configHashAnnotation]
	if currentHash != r.configHash {
		deployment.Spec.Template.Annotations[configHashAnnotation] = r.configHash
	}

	return deployment, nil
}

// calculateConfigHash calculates a hash of the configuration to trigger rolling updates
func calculateConfigHash(cr *uiv1alpha1.ScalityUIComponentExposer, ui *uiv1alpha1.ScalityUI, component *uiv1alpha1.ScalityUIComponent) string {
	hash := sha256.New()

	// Include exposer spec in hash
	hash.Write([]byte(fmt.Sprintf("%+v", cr.Spec)))

	// Include component status in hash (for PublicPath changes)
	hash.Write([]byte(fmt.Sprintf("%s-%s", component.Status.PublicPath, component.Status.Kind)))

	// Include UI name in hash
	hash.Write([]byte(ui.Name))

	return fmt.Sprintf("%x", hash.Sum(nil))[:16] // Use first 16 characters
}

// ensureConfigMapVolume ensures the ConfigMap volume exists in the deployment
func ensureConfigMapVolume(deployment *appsv1.Deployment, volumeName, configMapName string) bool {
	volumes := &deployment.Spec.Template.Spec.Volumes

	// Check if volume already exists
	for i, volume := range *volumes {
		if volume.Name == volumeName {
			// Update ConfigMap name if different
			if volume.ConfigMap == nil || volume.ConfigMap.Name != configMapName {
				(*volumes)[i].ConfigMap = &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				}
				return true
			}
			return false
		}
	}

	// Add new volume
	*volumes = append(*volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	})
	return true
}

// ensureConfigMapVolumeMount ensures the ConfigMap volume mount exists in the container
func ensureConfigMapVolumeMount(container *corev1.Container, volumeName, mountPath string) bool {
	volumeMounts := &container.VolumeMounts

	// Check if volume mount already exists
	for i, mount := range *volumeMounts {
		if mount.Name == volumeName {
			// Update mount path and settings if different
			changed := false
			if mount.MountPath != mountPath {
				(*volumeMounts)[i].MountPath = mountPath
				changed = true
			}
			if mount.SubPath != "" {
				(*volumeMounts)[i].SubPath = ""
				changed = true
			}
			if !mount.ReadOnly {
				(*volumeMounts)[i].ReadOnly = true
				changed = true
			}
			return changed
		}
	}

	// Add new volume mount
	*volumeMounts = append(*volumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
		SubPath:   "",
		ReadOnly:  true,
	})
	return true
}

// Dependencies returns the list of dependencies for this reconciler
func (r *scalityUIComponentExposerDeploymentReconciler) Dependencies() []string {
	return []string{"configmap"} // Depends on ConfigMap being ready
}

// AsHashable returns a hashable representation for comparison
func (r *scalityUIComponentExposerDeploymentReconciler) AsHashable(deployment *appsv1.Deployment) interface{} {
	if deployment == nil {
		return nil
	}

	return map[string]interface{}{
		"name":        deployment.Name,
		"namespace":   deployment.Namespace,
		"configHash":  r.configHash,
		"volumes":     deployment.Spec.Template.Spec.Volumes,
		"annotations": deployment.Spec.Template.Annotations,
	}
}

// IsNil checks if the Deployment is nil
func (r *scalityUIComponentExposerDeploymentReconciler) IsNil(deployment *appsv1.Deployment) bool {
	return deployment == nil
}

// PostReconcile is called after successful reconciliation
func (r *scalityUIComponentExposerDeploymentReconciler) PostReconcile(deployment *appsv1.Deployment) {
	// Set status condition for successful Deployment reconciliation
	setStatusCondition(r.CR, conditionTypeDeploymentReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "Deployment successfully updated with ConfigMap mount")
}

// Validate performs validation
func (r *scalityUIComponentExposerDeploymentReconciler) Validate() error {
	// Skip validation if dependencies not available
	if r.shouldSkip || r.component == nil {
		return nil // Don't fail validation, just skip
	}

	return nil
}

// IsSettled checks if the deployment is settled (we don't wait for deployment rollout)
func (r *scalityUIComponentExposerDeploymentReconciler) IsSettled(deployment *appsv1.Deployment) bool {
	return true // We don't wait for deployment rollout completion
}

// MergeResources handles merging of old and new deployment resources
func (r *scalityUIComponentExposerDeploymentReconciler) MergeResources(old *appsv1.Deployment, new *appsv1.Deployment) *appsv1.Deployment {
	// For deployment updates, we want to preserve the existing deployment and just update specific fields
	if old == nil {
		return new
	}

	// We're updating an existing deployment, so start with the old one
	merged := old.DeepCopy()

	// Apply our updates from the new deployment
	if new != nil {
		// Update volumes
		merged.Spec.Template.Spec.Volumes = new.Spec.Template.Spec.Volumes

		// Update container volume mounts
		for i, newContainer := range new.Spec.Template.Spec.Containers {
			if i < len(merged.Spec.Template.Spec.Containers) {
				merged.Spec.Template.Spec.Containers[i].VolumeMounts = newContainer.VolumeMounts
			}
		}

		// Update annotations
		if merged.Spec.Template.Annotations == nil {
			merged.Spec.Template.Annotations = make(map[string]string)
		}
		for k, v := range new.Spec.Template.Annotations {
			merged.Spec.Template.Annotations[k] = v
		}
	}

	return merged
}
