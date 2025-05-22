/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// ScalityUIComponentExposerReconciler reconciles a ScalityUIComponentExposer object
type ScalityUIComponentExposerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch

type MicroAppRuntimeConfiguration struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		ScalityUI          string                 `json:"scalityUI"`
		ScalityUIComponent string                 `json:"scalityUIComponent"`
		Title              string                 `json:"title"`
		Auth               map[string]interface{} `json:"auth,omitempty"`
	} `json:"spec"`
}

type DeployedUIApp struct {
	AppHistoryPath string `json:"appHistoryPath"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	Version        string `json:"version"`
}

const configMapKey = "runtime-app-configuration"
const configHashAnnotation = "ui.scality.com/config-hash"
const volumeNamePrefix = "config-volume-"

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalityUIComponentExposerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling ScalityUIComponentExposer", "namespace", req.Namespace, "name", req.Name)

	// Fetch the ScalityUIComponentExposer instance
	exposer := &uiv1alpha1.ScalityUIComponentExposer{}
	if err := r.Get(ctx, req.NamespacedName, exposer); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUIComponentExposer resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponentExposer", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, fmt.Errorf("failed to get ScalityUIComponentExposer: %w", err)
	}

	// Fetch the ScalityUIComponent
	component := &uiv1alpha1.ScalityUIComponent{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      exposer.Spec.ScalityUIComponent,
		Namespace: exposer.Namespace,
	}, component); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUIComponent not found, requeueing",
				"namespace", exposer.Namespace,
				"componentName", exposer.Spec.ScalityUIComponent)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponent",
			"namespace", exposer.Namespace,
			"componentName", exposer.Spec.ScalityUIComponent)
		return ctrl.Result{}, fmt.Errorf("failed to get ScalityUIComponent %s: %w",
			exposer.Spec.ScalityUIComponent, err)
	}

	// Fetch the ScalityUI
	ui := &uiv1alpha1.ScalityUI{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      exposer.Spec.ScalityUI,
		Namespace: exposer.Namespace,
	}, ui); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ScalityUI not found, requeueing",
				"namespace", exposer.Namespace,
				"uiName", exposer.Spec.ScalityUI)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get ScalityUI",
			"namespace", exposer.Namespace,
			"uiName", exposer.Spec.ScalityUI)
		return ctrl.Result{}, fmt.Errorf("failed to get ScalityUI %s: %w",
			exposer.Spec.ScalityUI, err)
	}

	if err := r.reconcileScalityUIExposerIngress(ctx, exposer, ui, component, logger); err != nil {
		logger.Error(err, "Failed to reconcile ScalityUI exposer Ingress",
			"namespace", exposer.Namespace,
			"name", exposer.Name)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile Ingress: %w", err)
	}

	configMap, err := r.reconcileScalityUIExposerConfigMap(ctx, exposer, ui, component, logger)
	if err != nil {
		logger.Error(err, "Failed to reconcile ScalityUI exposer ConfigMap",
			"namespace", exposer.Namespace,
			"name", exposer.Name)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile ConfigMap: %w", err)
	}

	configMapHash, err := r.calculateConfigMapHash(configMap)
	if err != nil {
		logger.Error(err, "Failed to calculate ConfigMap hash",
			"namespace", configMap.Namespace,
			"name", configMap.Name)
		return ctrl.Result{}, fmt.Errorf("failed to calculate ConfigMap hash: %w", err)
	}

	if err := r.updateComponentDeployment(ctx, component, exposer, configMapHash, logger); err != nil {
		logger.Error(err, "Failed to update component deployment",
			"namespace", component.Namespace,
			"name", component.Name)
		return ctrl.Result{}, fmt.Errorf("failed to update component deployment: %w", err)
	}

	if err := r.reconcileScalityUIDeployedApps(ctx, ui, component, exposer, logger); err != nil {
		logger.Error(err, "Failed to reconcile ScalityUI deployed-ui-apps ConfigMap",
			"namespace", ui.Namespace,
			"name", ui.Name)
		return ctrl.Result{}, fmt.Errorf("failed to reconcile deployed-ui-apps ConfigMap: %w", err)
	}

	logger.Info("Successfully reconciled ScalityUIComponentExposer",
		"namespace", req.Namespace,
		"name", req.Name)
	return ctrl.Result{}, nil
}

// reconcileScalityUIExposerIngress creates or updates the Ingress for a ScalityUIComponentExposer
func (r *ScalityUIComponentExposerReconciler) reconcileScalityUIExposerIngress(ctx context.Context, exposer *uiv1alpha1.ScalityUIComponentExposer, ui *uiv1alpha1.ScalityUI, component *uiv1alpha1.ScalityUIComponent, logger logr.Logger) error {
	// Check for existing conflicting Ingress before creating
	existingConflict, existingIngress, err := r.checkForIngressConflicts(ctx, exposer, component, logger)
	if err != nil {
		return fmt.Errorf("failed to check for Ingress conflicts: %w", err)
	}

	// If conflict was found, handle it accordingly
	if existingConflict {
		// Log the conflict but continue with fallback strategy
		logger.Info("Found conflicting Ingress path, using alternative path",
			"component", component.Name,
			"conflictingIngress", existingIngress)
	}

	// Create or update the Ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ingress", exposer.Name),
			Namespace: exposer.Namespace,
		},
	}

	logger.Info("Reconciling Ingress", "namespace", ingress.Namespace, "name", ingress.Name)

	// Determine path - if conflict was found, use a more unique path
	var path string
	if existingConflict {
		// Use a more unique path to avoid conflicts - prefix with exposer name
		path = fmt.Sprintf("/%s-%s", exposer.Name, component.Name)
	} else {
		path = fmt.Sprintf("/%s", component.Name)
	}

	_, err = ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		if err := ctrl.SetControllerReference(exposer, ingress, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference on Ingress: %w", err)
		}

		// Set labels
		if ingress.Labels == nil {
			ingress.Labels = make(map[string]string)
		}
		ingress.Labels["app.kubernetes.io/name"] = exposer.Name
		ingress.Labels["app.kubernetes.io/part-of"] = ui.Spec.ProductName
		ingress.Labels["app.kubernetes.io/component"] = component.Name

		// Set annotations
		if ingress.Annotations == nil {
			ingress.Annotations = make(map[string]string)
		}

		// Configure path
		pathType := networkingv1.PathTypePrefix

		// Build rules for the Ingress
		ingress.Spec = networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     path,
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: component.Name,
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update Ingress: %w", err)
	}

	logger.Info("Successfully reconciled Ingress", "namespace", ingress.Namespace, "name", ingress.Name, "path", path)
	return nil
}

// checkForIngressConflicts looks for existing Ingress resources that might conflict with the one we're creating
// Returns a boolean indicating if a conflict was found and the name of the conflicting Ingress
func (r *ScalityUIComponentExposerReconciler) checkForIngressConflicts(ctx context.Context, exposer *uiv1alpha1.ScalityUIComponentExposer, component *uiv1alpha1.ScalityUIComponent, logger logr.Logger) (bool, string, error) {
	// Determine the path we're planning to use
	path := fmt.Sprintf("/%s", component.Name)

	// Get all Ingresses in the namespace
	ingressList := &networkingv1.IngressList{}
	if err := r.List(ctx, ingressList, client.InNamespace(exposer.Namespace)); err != nil {
		return false, "", fmt.Errorf("failed to list Ingresses: %w", err)
	}

	ourIngressName := fmt.Sprintf("%s-ingress", exposer.Name)

	// Check each Ingress for conflicts
	for _, ing := range ingressList.Items {
		// Skip our own Ingress
		if ing.Name == ourIngressName {
			continue
		}

		// Check each rule
		for _, rule := range ing.Spec.Rules {
			// Skip if this rule has no HTTP value
			if rule.HTTP == nil {
				continue
			}

			// Check each path
			for _, httpPath := range rule.HTTP.Paths {
				if httpPath.Path == path {
					conflictName := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
					logger.Info("Found conflicting Ingress path",
						"path", path,
						"conflictingIngress", conflictName)
					return true, conflictName, nil
				}
			}
		}
	}

	return false, "", nil
}

// reconcileScalityUIExposerConfigMap creates or updates the ConfigMap for a ScalityUIComponentExposer
func (r *ScalityUIComponentExposerReconciler) reconcileScalityUIExposerConfigMap(ctx context.Context, exposer *uiv1alpha1.ScalityUIComponentExposer, ui *uiv1alpha1.ScalityUI, component *uiv1alpha1.ScalityUIComponent, logger logr.Logger) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exposer.Name,
			Namespace: exposer.Namespace,
		},
	}

	logger.Info("Reconciling ConfigMap", "namespace", configMap.Namespace, "name", configMap.Name)

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		if err := ctrl.SetControllerReference(exposer, configMap, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference on ConfigMap: %w", err)
		}
		if configMap.Labels == nil {
			configMap.Labels = make(map[string]string)
		}
		configMap.Labels["app.kubernetes.io/name"] = exposer.Name
		configMap.Labels["app.kubernetes.io/part-of"] = ui.Spec.ProductName
		configMap.Labels["app.kubernetes.io/component"] = component.Name

		isNewConfigMap := configMap.ResourceVersion == ""
		_, keyExists := configMap.Data[configMapKey]

		if isNewConfigMap || !keyExists {
			if configMap.Data == nil {
				configMap.Data = make(map[string]string)
			}
			runtimeConfig := MicroAppRuntimeConfiguration{
				Kind:       "MicroAppRuntimeConfiguration",
				APIVersion: "ui.scality.com/v1alpha1",
			}
			runtimeConfig.Metadata.Kind = component.Name
			runtimeConfig.Metadata.Name = fmt.Sprintf("%s.%s", component.Name, "eu-west-1")
			runtimeConfig.Spec.Title = component.Name
			runtimeConfig.Spec.ScalityUI = ui.Name
			runtimeConfig.Spec.ScalityUIComponent = component.Name
			runtimeConfig.Spec.Auth = map[string]interface{}{
				"kind":           "OIDC",
				"providerUrl":    "",
				"redirectUrl":    "/",
				"clientId":       "",
				"responseType":   "code",
				"scopes":         "openid email profile",
				"providerLogout": true,
			}
			configJSONBytes, errMarshal := json.MarshalIndent(runtimeConfig, "", "  ")
			if errMarshal != nil {
				return fmt.Errorf("failed to marshal runtime config: %w", errMarshal)
			}
			configMap.Data[configMapKey] = string(configJSONBytes)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}

	logger.Info("Successfully reconciled ConfigMap", "namespace", configMap.Namespace, "name", configMap.Name)
	return configMap, nil
}

// calculateConfigMapHash calculates the SHA-256 hash of the ConfigMap data
func (r *ScalityUIComponentExposerReconciler) calculateConfigMapHash(configMap *corev1.ConfigMap) (string, error) {
	dataToHash, dataKeyExists := configMap.Data[configMapKey]
	if !dataKeyExists {
		return "", fmt.Errorf("key '%s' missing from ConfigMap '%s'", configMapKey, configMap.Name)
	}
	hash := sha256.Sum256([]byte(dataToHash))
	return hex.EncodeToString(hash[:]), nil
}

// updateComponentDeployment updates the component's deployment to mount the ConfigMap
func (r *ScalityUIComponentExposerReconciler) updateComponentDeployment(ctx context.Context, component *uiv1alpha1.ScalityUIComponent, exposer *uiv1alpha1.ScalityUIComponentExposer, configMapHash string, logger logr.Logger) error {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      component.Name,
		Namespace: exposer.Namespace,
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Component deployment not found, skipping update",
				"namespace", exposer.Namespace,
				"name", component.Name)
			return nil
		}
		return fmt.Errorf("failed to get component deployment: %w", err)
	}

	logger.Info("Updating deployment with ConfigMap mount",
		"namespace", deployment.Namespace,
		"name", deployment.Name)

	_, err = ctrl.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		volumeName := volumeNamePrefix + component.Name

		// Define Volume
		foundVolume := false
		for i, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == volumeName {
				if vol.ConfigMap == nil || vol.ConfigMap.Name != exposer.Name {
					deployment.Spec.Template.Spec.Volumes[i].ConfigMap = &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: exposer.Name,
						},
					}
				}
				foundVolume = true
				break
			}
		}
		if !foundVolume {
			deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: exposer.Name,
						},
					},
				},
			})
		}

		// Define VolumeMount
		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]
			foundMount := false
			for j, mount := range container.VolumeMounts {
				if mount.Name == volumeName {
					if mount.MountPath != "/usr/share/nginx/html/.well-known/runtime-app-configuration" || mount.SubPath != configMapKey {
						deployment.Spec.Template.Spec.Containers[i].VolumeMounts[j].MountPath = "/usr/share/nginx/html/.well-known/runtime-app-configuration"
						deployment.Spec.Template.Spec.Containers[i].VolumeMounts[j].SubPath = configMapKey
						deployment.Spec.Template.Spec.Containers[i].VolumeMounts[j].ReadOnly = true
					}
					foundMount = true
					break
				}
			}
			if !foundMount {
				container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: "/usr/share/nginx/html/.well-known/runtime-app-configuration",
					SubPath:   configMapKey,
					ReadOnly:  true,
				})
			}
		}

		// Set annotation to trigger rolling update
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		oldHash := deployment.Spec.Template.Annotations[configHashAnnotation]
		if oldHash != configMapHash {
			deployment.Spec.Template.Annotations[configHashAnnotation] = configMapHash
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update deployment with ConfigMap mount: %w", err)
	}

	logger.Info("Successfully updated deployment with ConfigMap mount",
		"namespace", deployment.Namespace,
		"name", deployment.Name)
	return nil
}

// reconcileScalityUIDeployedApps updates or creates the deployed-ui-apps ConfigMap for ScalityUI
func (r *ScalityUIComponentExposerReconciler) reconcileScalityUIDeployedApps(ctx context.Context, ui *uiv1alpha1.ScalityUI, component *uiv1alpha1.ScalityUIComponent, exposer *uiv1alpha1.ScalityUIComponentExposer, logger logr.Logger) error {
	configMapName := fmt.Sprintf("%s-deployed-ui-apps", ui.Name)
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: ui.Namespace,
	}, configMap)

	if err != nil {
		return fmt.Errorf("failed to get deployed-ui-apps ConfigMap: %w", err)
	}

	logger.Info("Reconciling deployed-ui-apps ConfigMap",
		"namespace", configMap.Namespace,
		"name", configMap.Name)

	_, err = ctrl.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		if err := ctrl.SetControllerReference(ui, configMap, r.Scheme); err != nil {
			return fmt.Errorf("failed to set controller reference on deployed-ui-apps ConfigMap: %w", err)
		}

		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}

		// Get base URL from Ingress if available
		baseURL := determineComponentURL(component)

		// Create the component entry to be added to deployed-ui-apps.json
		componentEntry := map[string]interface{}{
			"appHistoryBasePath": exposer.Spec.AppHistoryPath,
			"kind":               component.Status.Kind,
			"name":               component.Name,
			"url":                baseURL,
			"version":            component.Status.Version,
		}

		// Get existing deployed-ui-apps.json data
		var allApps []map[string]interface{}
		existingData, hasExistingData := configMap.Data["deployed-ui-apps.json"]

		if hasExistingData {
			// Try to parse existing data
			if err := json.Unmarshal([]byte(existingData), &allApps); err != nil {
				logger.Error(err, "Failed to parse existing deployed-ui-apps.json, will create new data")
				allApps = []map[string]interface{}{}
			}
		} else {
			// Initialize new array if no existing data
			allApps = []map[string]interface{}{}
		}

		// Update or add the current component entry
		found := false
		for i, app := range allApps {
			if app["name"] == component.Name {
				allApps[i] = componentEntry
				found = true
				break
			}
		}

		if !found {
			allApps = append(allApps, componentEntry)
		}

		// Marshal the updated apps array
		allAppsJSON, err := json.MarshalIndent(allApps, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal deployed-ui-apps.json: %w", err)
		}

		// Update the ConfigMap with only the deployed-ui-apps.json field
		configMap.Data["deployed-ui-apps.json"] = string(allAppsJSON)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update deployed-ui-apps ConfigMap: %w", err)
	}

	logger.Info("Successfully reconciled deployed-ui-apps ConfigMap",
		"namespace", configMap.Namespace,
		"name", configMap.Name)
	return nil
}

// TODO: This is a temporary function to determine the URL for the component.
func determineComponentURL(component *uiv1alpha1.ScalityUIComponent) string {
	ingressPath := component.Status.PublicPath

	return ingressPath
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
