package scalityuicomponentexposer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// timestampFormat is the format used for timestamps in annotations
	// Uses a Kubernetes-safe format without special characters
	timestampFormat = "20060102T150405Z"
)

// newDeploymentUpdateReducer creates a StateReducer for Deployment update
func newDeploymentUpdateReducer(r *ScalityUIComponentExposerReconciler) StateReducer {
	return StateReducer{
		N: "deployment-update",
		F: func(cr ScalityUIComponentExposer, state State, log logr.Logger) (ctrl.Result, error) {
			return r.updateComponentDeployment(cr, state, log)
		},
	}
}

// updateComponentDeployment updates the component's deployment to mount the ConfigMap
func (r *ScalityUIComponentExposerReconciler) updateComponentDeployment(
	cr ScalityUIComponentExposer,
	state State,
	log logr.Logger,
) (ctrl.Result, error) {
	ctx := state.GetContext()

	// Get dependencies
	component, _, err := validateAndFetchDependencies(ctx, cr, state, log)
	if err != nil {
		log.Info("Skipping deployment update due to missing dependencies", "error", err.Error())
		return ctrl.Result{}, nil
	}

	// Get ConfigMap hash from memory (set by configmap reducer)
	// This avoids cache sync issues when reading from Kubernetes API
	configMapHash, ok := state.GetSubresourceHash(configMapHashKey)
	if !ok || configMapHash == "" {
		log.Info("ConfigMap hash not found in memory, skipping deployment update")
		return ctrl.Result{}, nil
	}

	// Update deployment
	if err := r.updateComponentDeploymentInternal(ctx, component, cr, configMapHash, state, log); err != nil {
		log.Error(err, "Failed to update component deployment")
		setStatusCondition(cr, conditionTypeDeploymentReady, metav1.ConditionFalse,
			reasonReconcileFailed, fmt.Sprintf("Failed to update deployment: %v", err))
		return ctrl.Result{}, fmt.Errorf("failed to update component deployment: %w", err)
	}

	setStatusCondition(cr, conditionTypeDeploymentReady, metav1.ConditionTrue,
		reasonReconcileSucceeded, "Deployment successfully updated with ConfigMap mount")

	return ctrl.Result{}, nil
}

// updateComponentDeploymentInternal does the actual deployment update
func (r *ScalityUIComponentExposerReconciler) updateComponentDeploymentInternal(
	ctx context.Context,
	component *uiv1alpha1.ScalityUIComponent,
	exposer ScalityUIComponentExposer,
	configMapHash string,
	state State,
	log logr.Logger,
) error {
	deployment := &appsv1.Deployment{}
	err := state.GetKubeClient().Get(ctx, types.NamespacedName{
		Name:      component.Name,
		Namespace: exposer.GetNamespace(),
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Component deployment not found, skipping update",
				"deployment", component.Name)
			return nil
		}
		return fmt.Errorf("failed to get component deployment: %w", err)
	}

	log.Info("Updating deployment with ConfigMap mount", "deployment", deployment.Name)

	configChanged := false

	result, err := ctrl.CreateOrUpdate(ctx, state.GetKubeClient(), deployment, func() error {
		volumeName := volumeNamePrefix + component.Name
		configMapName := fmt.Sprintf("%s-%s", component.Name, configMapNameSuffix)

		// Update or add the ConfigMap volume
		configChanged = r.ensureConfigMapVolume(deployment, volumeName, configMapName) || configChanged

		// Update or add the ConfigMap volume mount for each container
		// Mount to configsSubdirectory to avoid overwriting the original micro-app-configuration file
		configsMountPath := component.Spec.MountPath + "/" + configsSubdirectory
		for i := range deployment.Spec.Template.Spec.Containers {
			configChanged = r.ensureConfigMapVolumeMount(&deployment.Spec.Template.Spec.Containers[i], volumeName, configsMountPath) || configChanged
		}

		// Set annotation to trigger rolling update if configuration changed or hash is different
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}

		// Update annotation if config changed or if hash is different
		currentHashAnnotation := deployment.Spec.Template.Annotations[configHashAnnotation]

		currentHash := ""
		if currentHashAnnotation != "" {
			if idx := strings.Index(currentHashAnnotation, "-"); idx >= 0 {
				currentHash = currentHashAnnotation[:idx]
			} else {
				currentHash = currentHashAnnotation
			}
		}

		if configChanged || currentHash != configMapHash {
			// Force a unique value by appending a timestamp to ensure pod restart
			timestamp := time.Now().Format(timestampFormat)
			deployment.Spec.Template.Annotations[configHashAnnotation] = configMapHash + "-" + timestamp
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update deployment with ConfigMap mount: %w", err)
	}

	log.Info("Successfully updated deployment with ConfigMap mount",
		"deployment", deployment.Name, "configChanged", configChanged, "operation", result)
	return nil
}

// ensureConfigMapVolume ensures the deployment has the specified ConfigMap volume
// Returns true if the volume was added or modified
func (r *ScalityUIComponentExposerReconciler) ensureConfigMapVolume(deployment *appsv1.Deployment, volumeName, configMapName string) bool {
	for i, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName {
			// Update existing volume if needed
			if vol.ConfigMap == nil || vol.ConfigMap.Name != configMapName {
				deployment.Spec.Template.Spec.Volumes[i].ConfigMap = &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				}
				return true
			}
			return false // Volume exists and is correctly configured
		}
	}

	// Add new volume
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
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

// ensureConfigMapVolumeMount ensures the container has the specified volume mount
// Returns true if the mount was added or modified
func (r *ScalityUIComponentExposerReconciler) ensureConfigMapVolumeMount(container *corev1.Container, volumeName, mountPath string) bool {
	// Check for existing mount and update if needed
	for i, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			// Check if mount configuration needs update
			needsUpdate := mount.MountPath != mountPath ||
				!mount.ReadOnly ||
				mount.SubPath != ""

			if needsUpdate {
				container.VolumeMounts[i].MountPath = mountPath
				container.VolumeMounts[i].ReadOnly = true
				container.VolumeMounts[i].SubPath = ""
				return true
			}
			return false // Mount exists and is correctly configured
		}
	}

	// Add new mount
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
		ReadOnly:  true,
	})
	return true
}
