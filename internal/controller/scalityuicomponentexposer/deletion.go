package scalityuicomponentexposer

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// handleDeletion handles the cleanup when an Exposer is being deleted.
// It removes the volume from the component deployment, cleans up the ConfigMap,
// and removes the finalizer from the Exposer.
func (r *ScalityUIComponentExposerReconciler) handleDeletion(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Handling deletion for ScalityUIComponentExposer")

	// Get the component name from spec (we need this even if component doesn't exist anymore)
	componentName := exposer.Spec.ScalityUIComponent
	namespace := exposer.Namespace

	// Step 1: Remove volume and volumeMount from component deployment
	if err := r.removeVolumeFromDeployment(ctx, namespace, componentName, log); err != nil {
		log.Error(err, "Failed to remove volume from deployment")
		return ctrl.Result{}, err
	}

	// Step 2: Clean up ConfigMap (remove data entry and finalizer)
	if err := r.cleanupConfigMap(ctx, namespace, componentName, exposer.Name, log); err != nil {
		log.Error(err, "Failed to cleanup ConfigMap")
		return ctrl.Result{}, err
	}

	// Step 3: Remove finalizer from Exposer
	if controllerutil.ContainsFinalizer(exposer, exposerFinalizer) {
		controllerutil.RemoveFinalizer(exposer, exposerFinalizer)
		if err := r.Client.Update(ctx, exposer); err != nil {
			log.Error(err, "Failed to remove finalizer from Exposer")
			return ctrl.Result{}, err
		}
		log.Info("Removed finalizer from Exposer")
	}

	log.Info("Deletion cleanup completed successfully")
	return ctrl.Result{}, nil
}

// removeVolumeFromDeployment removes the config volume and volumeMount from the component deployment
func (r *ScalityUIComponentExposerReconciler) removeVolumeFromDeployment(
	ctx context.Context,
	namespace, componentName string,
	log logr.Logger,
) error {
	deployment := &appsv1.Deployment{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      componentName,
		Namespace: namespace,
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Component deployment not found, skipping volume removal", "deployment", componentName)
			return nil
		}
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	volumeName := volumeNamePrefix + componentName
	volumeRemoved := false
	mountRemoved := false

	// Remove the volume
	newVolumes := make([]corev1.Volume, 0, len(deployment.Spec.Template.Spec.Volumes))
	for _, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName {
			volumeRemoved = true
			continue
		}
		newVolumes = append(newVolumes, vol)
	}
	deployment.Spec.Template.Spec.Volumes = newVolumes

	// Remove the volumeMount from all containers
	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		newMounts := make([]corev1.VolumeMount, 0, len(container.VolumeMounts))
		for _, mount := range container.VolumeMounts {
			if mount.Name == volumeName {
				mountRemoved = true
				continue
			}
			newMounts = append(newMounts, mount)
		}
		container.VolumeMounts = newMounts
	}

	if !volumeRemoved && !mountRemoved {
		log.Info("Volume and mount already removed from deployment", "deployment", componentName)
		return nil
	}

	// Update the hash annotation to trigger a rolling update.
	// We use "removed-" prefix (instead of a hash) to clearly indicate this is a cleanup operation,
	// distinguishing it from normal config updates which use "hash-timestamp" format.
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	timestamp := time.Now().Format(timestampFormat)
	deployment.Spec.Template.Annotations[configHashAnnotation] = "removed-" + timestamp

	if err := r.Client.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to update deployment after volume removal: %w", err)
	}

	log.Info("Removed volume and volumeMount from deployment",
		"deployment", componentName, "volume", volumeName)
	return nil
}

// cleanupConfigMap removes the exposer's data entry and finalizer from the ConfigMap
func (r *ScalityUIComponentExposerReconciler) cleanupConfigMap(
	ctx context.Context,
	namespace, componentName, exposerName string,
	log logr.Logger,
) error {
	configMapName := fmt.Sprintf("%s-%s", componentName, configMapNameSuffix)
	configMap := &corev1.ConfigMap{}

	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: namespace,
	}, configMap)

	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("ConfigMap not found, skipping cleanup", "configMap", configMapName)
			return nil
		}
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	finalizerName := configMapFinalizerPrefix + exposerName
	dataKeyRemoved := false
	finalizerRemoved := false

	// Remove the exposer's data entry from ConfigMap
	if configMap.Data != nil {
		if _, exists := configMap.Data[exposerName]; exists {
			delete(configMap.Data, exposerName)
			dataKeyRemoved = true
		}
	}

	// Remove the exposer's finalizer from ConfigMap
	if controllerutil.ContainsFinalizer(configMap, finalizerName) {
		controllerutil.RemoveFinalizer(configMap, finalizerName)
		finalizerRemoved = true
	}

	if !dataKeyRemoved && !finalizerRemoved {
		log.Info("ConfigMap already cleaned up", "configMap", configMapName)
		return nil
	}

	// Always update first to remove the finalizer and data entry
	if err := r.Client.Update(ctx, configMap); err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}
	log.Info("Cleaned up ConfigMap entry and finalizer",
		"configMap", configMapName, "exposer", exposerName,
		"remainingEntries", len(configMap.Data), "remainingFinalizers", len(configMap.Finalizers))

	// Check if ConfigMap should be deleted (no more data entries and no more finalizers)
	if len(configMap.Data) == 0 && len(configMap.Finalizers) == 0 {
		// Delete the ConfigMap since it's empty and has no finalizers
		if err := r.Client.Delete(ctx, configMap); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete empty ConfigMap: %w", err)
			}
		}
		log.Info("Deleted empty ConfigMap", "configMap", configMapName)
	}

	return nil
}

// ensureFinalizer adds the exposer finalizer if it's not already present.
// Returns true if the finalizer was added (and the object was updated).
func (r *ScalityUIComponentExposerReconciler) ensureFinalizer(
	ctx context.Context,
	exposer *uiv1alpha1.ScalityUIComponentExposer,
	log logr.Logger,
) (bool, error) {
	if !controllerutil.ContainsFinalizer(exposer, exposerFinalizer) {
		controllerutil.AddFinalizer(exposer, exposerFinalizer)
		if err := r.Client.Update(ctx, exposer); err != nil {
			return false, fmt.Errorf("failed to add finalizer: %w", err)
		}
		log.Info("Added finalizer to Exposer")
		return true, nil
	}
	return false, nil
}

// isBeingDeleted checks if the Exposer is being deleted
func isBeingDeleted(exposer *uiv1alpha1.ScalityUIComponentExposer) bool {
	return !exposer.DeletionTimestamp.IsZero()
}
