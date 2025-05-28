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
	"encoding/json"
	"fmt"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

const (
	uiServicePort    = 80
	defaultNamespace = "scality-ui"
)

// createConfigJSON creates a JSON config from the ScalityUI object
func createConfigJSON(scalityui *uiscalitycomv1alpha1.ScalityUI) ([]byte, error) {
	configOutput := make(map[string]interface{})

	// Basic fields
	configOutput["productName"] = scalityui.Spec.ProductName
	configOutput["discoveryUrl"] = "/shell/deployed-ui-apps.json"

	// Navbar configuration
	navbarData := make(map[string]interface{})
	navbarData["main"] = convertNavbarItems(scalityui.Spec.Navbar.Main)
	navbarData["subLogin"] = convertNavbarItems(scalityui.Spec.Navbar.SubLogin)
	configOutput["navbar"] = navbarData

	// Themes configuration
	// If Spec.Themes is its zero value (e.g. 'themes' block not provided in CR),
	// apply default themes. Otherwise, use the themes from the spec.
	themesData := make(map[string]interface{})
	if scalityui.Spec.Themes == (uiscalitycomv1alpha1.Themes{}) { // Check against zero value for the Themes struct
		themesData["light"] = map[string]interface{}{
			"type":     "core-ui",
			"name":     "artescaLight",
			"logoPath": "",
		}
		themesData["dark"] = map[string]interface{}{
			"type":     "core-ui",
			"name":     "darkRebrand",
			"logoPath": "",
		}
	} else {
		// User has provided the 'themes' block; use values from spec.
		themesData["light"] = convertTheme(scalityui.Spec.Themes.Light)
		themesData["dark"] = convertTheme(scalityui.Spec.Themes.Dark)
	}
	configOutput["themes"] = themesData

	return json.Marshal(configOutput)
}

// convertNavbarItems converts NavbarItem structs to the expected JSON format
func convertNavbarItems(items []uiscalitycomv1alpha1.NavbarItem) []map[string]interface{} {
	if items == nil {
		return []map[string]interface{}{}
	}

	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item.Internal != nil {
			configItem := map[string]interface{}{
				"kind": item.Internal.Kind,
				"view": item.Internal.View,
			}
			if len(item.Internal.Groups) > 0 {
				configItem["groups"] = item.Internal.Groups
			}
			if item.Internal.Icon != "" {
				configItem["icon"] = item.Internal.Icon
			}
			if len(item.Internal.Label) > 0 {
				configItem["label"] = item.Internal.Label
			}
			result = append(result, configItem)
		} else if item.External != nil {
			configItem := map[string]interface{}{
				"isExternal": true,
				"url":        item.External.URL,
			}
			if len(item.External.Groups) > 0 {
				configItem["groups"] = item.External.Groups
			}
			if item.External.Icon != "" {
				configItem["icon"] = item.External.Icon
			}
			if len(item.External.Label) > 0 {
				configItem["label"] = item.External.Label
			}
			result = append(result, configItem)
		}
	}
	return result
}

// convertTheme converts a Theme struct to the expected JSON format
func convertTheme(theme uiscalitycomv1alpha1.Theme) map[string]interface{} {
	result := map[string]interface{}{
		"type":     theme.Type,
		"name":     theme.Name,
		"logoPath": theme.Logo.Value,
	}
	return result
}

// createOrUpdateOwnedConfigMap creates or updates a ConfigMap with the given data and sets an owner reference.
func (r *ScalityUIReconciler) createOrUpdateOwnedConfigMap(ctx context.Context, owner metav1.Object, configMapToManage *corev1.ConfigMap, data map[string]string) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, configMapToManage, func() error {
		if err := controllerutil.SetControllerReference(owner, configMapToManage, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on ConfigMap %s/%s: %w", configMapToManage.Namespace, configMapToManage.Name, err)
		}
		configMapToManage.Data = data
		return nil
	})
}

// createOrUpdateDeployment creates or updates a Deployment with the given configuration
func (r *ScalityUIReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appsv1.Deployment, scalityui *uiscalitycomv1alpha1.ScalityUI, configHash string, deployedAppsHash string) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		if err := controllerutil.SetControllerReference(scalityui, deploy, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		}

		// Ensure selector and basic strategy
		if deploy.Spec.Selector == nil {
			deploy.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: make(map[string]string),
			}
		}
		deploy.Spec.Selector.MatchLabels["app"] = scalityui.Name

		one := int32(1)
		if deploy.Spec.Replicas == nil {
			deploy.Spec.Replicas = &one
		} else {
			*deploy.Spec.Replicas = one
		}

		zeroIntStr := intstr.FromInt(0)
		oneIntStr := intstr.FromInt(1)
		if deploy.Spec.Strategy.Type == "" {
			deploy.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
		}
		if deploy.Spec.Strategy.RollingUpdate == nil {
			deploy.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
				MaxUnavailable: &zeroIntStr,
				MaxSurge:       &oneIntStr,
			}
		} else {
			if deploy.Spec.Strategy.RollingUpdate.MaxUnavailable == nil {
				deploy.Spec.Strategy.RollingUpdate.MaxUnavailable = &zeroIntStr
			}
			if deploy.Spec.Strategy.RollingUpdate.MaxSurge == nil {
				deploy.Spec.Strategy.RollingUpdate.MaxSurge = &oneIntStr
			}
		}

		// Manage PodTemplate Labels
		if deploy.Spec.Template.Labels == nil {
			deploy.Spec.Template.Labels = make(map[string]string)
		}
		deploy.Spec.Template.Labels["app"] = scalityui.Name

		// Manage PodTemplate Annotations
		if deploy.Spec.Template.Annotations == nil {
			deploy.Spec.Template.Annotations = make(map[string]string)
		}
		deploy.Spec.Template.Annotations["checksum/config"] = configHash
		if deployedAppsHash != "" {
			deploy.Spec.Template.Annotations["checksum/deployed-ui-apps"] = deployedAppsHash
		}

		// Volume definitions
		configVolumeName := scalityui.Name + "-config-volume"
		deployedAppsVolumeName := scalityui.Name + "-deployed-ui-apps-volume"
		configMapDeployedAppsName := scalityui.Name + "-deployed-ui-apps"

		configVolume := corev1.Volume{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: scalityui.Name, // ConfigMap for config.json
					},
				},
			},
		}
		deployedAppsVolume := corev1.Volume{
			Name: deployedAppsVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapDeployedAppsName, // ConfigMap for deployed-ui-apps.json
					},
				},
			},
		}

		// Ensure volumes exist
		volumes := &deploy.Spec.Template.Spec.Volumes
		*volumes = ensureVolume(*volumes, configVolume)
		*volumes = ensureVolume(*volumes, deployedAppsVolume)

		// Container and VolumeMount definitions
		mountPath := "/usr/share/nginx/html/shell"
		containerName := scalityui.Name

		configVolumeMount := corev1.VolumeMount{
			Name:      configVolumeName,
			MountPath: filepath.Join(mountPath, "config.json"),
			SubPath:   "config.json",
		}
		deployedAppsVolumeMount := corev1.VolumeMount{
			Name:      deployedAppsVolumeName,
			MountPath: filepath.Join(mountPath, "deployed-ui-apps.json"),
			SubPath:   "deployed-ui-apps.json",
		}

		// Find or create the main container
		var mainContainer *corev1.Container
		containerFound := false
		for i := range deploy.Spec.Template.Spec.Containers {
			if deploy.Spec.Template.Spec.Containers[i].Name == containerName {
				mainContainer = &deploy.Spec.Template.Spec.Containers[i]
				containerFound = true
				break
			}
		}

		if !containerFound {
			deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, corev1.Container{Name: containerName})
			mainContainer = &deploy.Spec.Template.Spec.Containers[len(deploy.Spec.Template.Spec.Containers)-1]
		}

		// Set image for the main container
		mainContainer.Image = scalityui.Spec.Image

		// Ensure volume mounts exist in the main container
		mounts := &mainContainer.VolumeMounts
		*mounts = ensureVolumeMount(*mounts, configVolumeMount)
		*mounts = ensureVolumeMount(*mounts, deployedAppsVolumeMount)

		return nil
	})
}

// createOrUpdateIngress creates or updates an Ingress with the given configuration
func (r *ScalityUIReconciler) createOrUpdateIngress(ctx context.Context, ingress *networkingv1.Ingress, scalityui *uiscalitycomv1alpha1.ScalityUI) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		if err := controllerutil.SetControllerReference(scalityui, ingress, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Ingress %s/%s: %w", ingress.Namespace, ingress.Name, err)
		}

		pathType := networkingv1.PathTypePrefix

		// Set annotations if provided
		if len(scalityui.Spec.Networks.IngressAnnotations) > 0 {
			if ingress.Annotations == nil {
				ingress.Annotations = make(map[string]string)
			}
			for key, value := range scalityui.Spec.Networks.IngressAnnotations {
				ingress.Annotations[key] = value
			}
		}

		// Create the basic ingress rule
		ingressRule := networkingv1.IngressRule{
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: scalityui.Name,
									Port: networkingv1.ServiceBackendPort{
										Number: 80,
									},
								},
							},
						},
					},
				},
			},
		}

		// Set host if provided
		if scalityui.Spec.Networks.Host != "" {
			ingressRule.Host = scalityui.Spec.Networks.Host
		}

		ingress.Spec = networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{ingressRule},
		}

		// Set IngressClassName if provided
		if scalityui.Spec.Networks.IngressClassName != "" {
			ingress.Spec.IngressClassName = &scalityui.Spec.Networks.IngressClassName
		}

		// Add TLS configuration if provided
		if len(scalityui.Spec.Networks.TLS) > 0 {
			ingress.Spec.TLS = make([]networkingv1.IngressTLS, len(scalityui.Spec.Networks.TLS))
			for i, tls := range scalityui.Spec.Networks.TLS {
				ingress.Spec.TLS[i] = networkingv1.IngressTLS{
					Hosts:      tls.Hosts,
					SecretName: tls.SecretName,
				}
			}
		}

		return nil
	})
}

// ensureVolume checks if a volume exists in the slice and updates/adds it.
// It returns the modified slice of volumes.
func ensureVolume(volumes []corev1.Volume, desiredVolume corev1.Volume) []corev1.Volume {
	for i, vol := range volumes {
		if vol.Name == desiredVolume.Name {
			volumes[i] = desiredVolume // Update existing volume
			return volumes
		}
	}
	return append(volumes, desiredVolume) // Add new volume
}

// ensureVolumeMount checks if a volumeMount exists in the slice and updates/adds it.
// It returns the modified slice of volumeMounts.
func ensureVolumeMount(volumeMounts []corev1.VolumeMount, desiredMount corev1.VolumeMount) []corev1.VolumeMount {
	for i, mount := range volumeMounts {
		if mount.Name == desiredMount.Name {
			volumeMounts[i] = desiredMount // Update existing mount
			return volumeMounts
		}
	}
	return append(volumeMounts, desiredMount) // Add new mount
}

// createOrUpdateService creates or updates a Service for the ScalityUI deployment.
func (r *ScalityUIReconciler) createOrUpdateService(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (controllerutil.OperationResult, error) {
	log := log.FromContext(ctx)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityui.Name,
			Namespace: defaultNamespace,
		},
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		if err := controllerutil.SetControllerReference(scalityui, service, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Service %s/%s: %w", service.Namespace, service.Name, err)
		}

		// Log the attempt to create or update the service
		log.Info("Ensuring Service exists", "service", service.Name)

		service.Spec.Selector = map[string]string{
			"app": scalityui.Name,
		}
		if len(service.Spec.Ports) == 0 {
			service.Spec.Ports = []corev1.ServicePort{
				{
					Name:       "http",
					Port:       uiServicePort,
					TargetPort: intstr.FromInt(uiServicePort),
					Protocol:   corev1.ProtocolTCP,
				},
			}
		} else {
			portFound := false
			for i, port := range service.Spec.Ports {
				if port.Name == "http" {
					service.Spec.Ports[i].Port = uiServicePort
					service.Spec.Ports[i].TargetPort = intstr.FromInt(uiServicePort)
					service.Spec.Ports[i].Protocol = corev1.ProtocolTCP
					portFound = true
					break
				}
			}
			if !portFound {
				service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
					Name:       "http",
					Port:       uiServicePort,
					TargetPort: intstr.FromInt(uiServicePort),
					Protocol:   corev1.ProtocolTCP,
				})
			}
		}
		service.Spec.Type = corev1.ServiceTypeClusterIP // Default service type

		return nil
	})

	if err != nil {
		return opResult, fmt.Errorf("failed to create or update Service %s/%s: %w", service.Namespace, service.Name, err)
	}

	logOperationResult(log, opResult, "Service", service.Name) // Use the helper to log results
	return opResult, nil
}

// ScalityUIReconciler reconciles a ScalityUI object
type ScalityUIReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ScalityUI object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *ScalityUIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	scalityui := &uiscalitycomv1alpha1.ScalityUI{}
	err := r.Client.Get(ctx, req.NamespacedName, scalityui)

	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "Unable to fetch ScalityUI")
			return ctrl.Result{}, err
		}
		// Resource deleted, no action needed
		return ctrl.Result{}, nil
	}

	// Generate config JSON
	configJSON, err := createConfigJSON(scalityui)
	if err != nil {
		r.Log.Error(err, "Failed to create configJSON")
		return ctrl.Result{}, err
	}

	// Calculate hash of configJSON
	h := sha256.New()
	h.Write(configJSON)
	configHash := fmt.Sprintf("%x", h.Sum(nil))

	// Calculate hash of deployed-ui-apps ConfigMap
	deployedAppsHash, err := r.calculateDeployedAppsHash(ctx, scalityui)
	if err != nil {
		r.Log.Error(err, "Failed to calculate deployed-ui-apps hash")
		// Continue without the hash - this is not critical
		deployedAppsHash = ""
	}

	// Define ConfigMap for config.json
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityui.Name,
			Namespace: defaultNamespace,
		},
	}
	configJsonData := map[string]string{"config.json": string(configJSON)}

	// Create or update ConfigMap for config.json
	configMapResult, err := r.createOrUpdateOwnedConfigMap(ctx, scalityui, configMap, configJsonData)
	if err != nil {
		r.Log.Error(err, "Failed to create or update ConfigMap for config.json")
		return ctrl.Result{}, err
	}

	logOperationResult(r.Log, configMapResult, "ConfigMap config.json", configMap.Name)

	// Define and manage ConfigMap for deployed-ui-apps.json
	configMapDeployedAppsName := scalityui.Name + "-deployed-ui-apps"
	configMapDeployedApps := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapDeployedAppsName,
			Namespace: defaultNamespace,
		},
	}

	// Use CreateOrUpdate to ensure the ConfigMap exists and has the correct owner reference.
	// The data for "deployed-ui-apps.json" should only be initialized if it's missing.
	opResultDeployedApps, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMapDeployedApps, func() error {
		if err := controllerutil.SetControllerReference(scalityui, configMapDeployedApps, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on ConfigMap %s/%s: %w", configMapDeployedApps.Namespace, configMapDeployedApps.Name, err)
		}

		// Initialize Data map if nil
		if configMapDeployedApps.Data == nil {
			configMapDeployedApps.Data = make(map[string]string)
		}

		// Only set "deployed-ui-apps.json" if it doesn't already exist.
		// This allows other controllers (like ScalityUIComponentExposerReconciler) to manage its content.
		if _, ok := configMapDeployedApps.Data["deployed-ui-apps.json"]; !ok {
			configMapDeployedApps.Data["deployed-ui-apps.json"] = "[]" // Default to empty JSON array
		}
		return nil
	})

	if err != nil {
		r.Log.Error(err, "Failed to create or update ConfigMap for deployed-ui-apps.json", "name", configMapDeployedApps.Name)
		return ctrl.Result{}, err
	}

	logOperationResult(r.Log, opResultDeployedApps, "ConfigMap deployed-ui-apps.json", configMapDeployedApps.Name)

	// Define Deployment
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityui.Name,
			Namespace: defaultNamespace,
		},
	}

	// Create or update Deployment
	deploymentResult, err := r.createOrUpdateDeployment(ctx, deploy, scalityui, configHash, deployedAppsHash)
	if err != nil {
		r.Log.Error(err, "Failed to create or update deployment")
		return ctrl.Result{}, err
	}

	// Log Deployment operation result
	switch deploymentResult {
	case controllerutil.OperationResultCreated:
		r.Log.Info("Deployment created", "name", deploy.Name)
	case controllerutil.OperationResultUpdated:
		r.Log.Info("Deployment updated", "name", deploy.Name)
	case controllerutil.OperationResultNone:
		r.Log.Info("Deployment unchanged", "name", deploy.Name)
	default:
		r.Log.Info("Deployment status", "name", deploy.Name, "result", deploymentResult)
	}

	// Verify Deployment exists
	existingDeployment := &appsv1.Deployment{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: deploy.Name, Namespace: deploy.Namespace}, existingDeployment)
	if err != nil {
		r.Log.Error(err, "Deployment was not created successfully")
		return ctrl.Result{}, err
	}
	r.Log.Info("Deployment exists", "name", existingDeployment.Name)

	// Create or update Service first (before Ingress, since Ingress references the Service)
	serviceResult, err := r.createOrUpdateService(ctx, scalityui)
	if err != nil {
		r.Log.Error(err, "Failed to create or update Service")
		return ctrl.Result{}, err
	}
	logOperationResult(r.Log, serviceResult, "Service", scalityui.Name)

	// Always create Ingress (either with Networks configuration or default)
	{
		// Define Ingress
		ingress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      scalityui.Name,
				Namespace: defaultNamespace,
			},
		}

		// Create or update Ingress
		ingressResult, err := r.createOrUpdateIngress(ctx, ingress, scalityui)
		if err != nil {
			r.Log.Error(err, "Failed to create or update Ingress")
			return ctrl.Result{}, err
		}

		// Log Ingress operation result
		switch ingressResult {
		case controllerutil.OperationResultCreated:
			r.Log.Info("Ingress created", "name", ingress.Name)
		case controllerutil.OperationResultUpdated:
			r.Log.Info("Ingress updated", "name", ingress.Name)
		case controllerutil.OperationResultNone:
			r.Log.Info("Ingress unchanged", "name", ingress.Name)
		}

		// Verify Ingress exists
		existingIngress := &networkingv1.Ingress{}
		err = r.Client.Get(ctx, client.ObjectKey{Name: ingress.Name, Namespace: ingress.Namespace}, existingIngress)
		if err != nil {
			r.Log.Error(err, "Ingress was not created successfully")
			return ctrl.Result{}, err
		}
		r.Log.Info("Ingress exists", "name", existingIngress.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiscalitycomv1alpha1.ScalityUI{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}

// calculateDeployedAppsHash calculates the hash of the deployed-ui-apps ConfigMap
func (r *ScalityUIReconciler) calculateDeployedAppsHash(ctx context.Context, scalityui *uiscalitycomv1alpha1.ScalityUI) (string, error) {
	configMapName := scalityui.Name + "-deployed-ui-apps"
	configMap := &corev1.ConfigMap{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name:      configMapName,
		Namespace: defaultNamespace,
	}, configMap)

	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return "", fmt.Errorf("failed to get deployed-ui-apps ConfigMap: %w", err)
		}
		// ConfigMap doesn't exist yet, return empty hash
		return "", nil
	}

	// Calculate hash of the deployed-ui-apps.json content
	deployedAppsData, exists := configMap.Data["deployed-ui-apps.json"]
	if !exists {
		return "", nil
	}

	h := sha256.New()
	h.Write([]byte(deployedAppsData))
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Helper function to log operation results
func logOperationResult(logger logr.Logger, result controllerutil.OperationResult, resourceType string, resourceName string) {
	switch result {
	case controllerutil.OperationResultCreated:
		logger.Info(fmt.Sprintf("%s created", resourceType), "name", resourceName)
	case controllerutil.OperationResultUpdated:
		logger.Info(fmt.Sprintf("%s updated", resourceType), "name", resourceName)
	case controllerutil.OperationResultNone:
		logger.Info(fmt.Sprintf("%s unchanged", resourceType), "name", resourceName)
	default:
		logger.Info(fmt.Sprintf("%s status", resourceType), "name", resourceName, "result", result)
	}
}
