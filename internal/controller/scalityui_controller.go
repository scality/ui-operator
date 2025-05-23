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
	DefaultScalityUIName      = "scality-ui"
	DefaultScalityUINamespace = "ui"
)

// createConfigJSON creates a JSON config from the ScalityUI object
func createConfigJSON(scalityui *uiscalitycomv1alpha1.ScalityUI) ([]byte, error) {
	configOutput := make(map[string]interface{})

	// Basic fields
	configOutput["productName"] = scalityui.Spec.ProductName
	configOutput["discoveryUrl"] = "/shell/deployed-ui-apps.json" // Static value as requested

	// Optional simple fields from spec
	configOutput["canChangeTheme"] = scalityui.Spec.CanChangeTheme
	configOutput["canChangeInstanceName"] = scalityui.Spec.CanChangeInstanceName
	configOutput["canChangeLanguage"] = scalityui.Spec.CanChangeLanguage
	// For omitempty string fields, if they are empty in spec, they will be empty strings in JSON
	configOutput["favicon"] = scalityui.Spec.Favicon
	configOutput["logo"] = scalityui.Spec.Logo

	// Navbar configuration
	// If Spec.Navbar is its zero value (e.g. not provided in CR),
	// its Main and SubLogin slices will be nil.
	navbarData := make(map[string]interface{})
	if scalityui.Spec.Navbar.Main == nil {
		navbarData["main"] = []uiscalitycomv1alpha1.NavbarItem{}
	} else {
		navbarData["main"] = scalityui.Spec.Navbar.Main
	}

	if scalityui.Spec.Navbar.SubLogin == nil {
		navbarData["subLogin"] = []uiscalitycomv1alpha1.NavbarItem{}
	} else {
		navbarData["subLogin"] = scalityui.Spec.Navbar.SubLogin
	}
	configOutput["navbar"] = navbarData

	// Themes configuration
	// If Spec.Themes is its zero value (e.g. 'themes' block not provided in CR),
	// apply default themes. Otherwise, use the themes from the spec.
	themesData := make(map[string]interface{})
	if scalityui.Spec.Themes == (uiscalitycomv1alpha1.Themes{}) { // Check against zero value for the Themes struct
		themesData["light"] = uiscalitycomv1alpha1.Theme{
			Type:     "core-ui",
			Name:     "artescaLight",
			LogoPath: "",
		}
		themesData["dark"] = uiscalitycomv1alpha1.Theme{
			Type:     "core-ui",
			Name:     "darkRebrand",
			LogoPath: "",
		}
	} else {
		// User has provided the 'themes' block; use values from spec.
		// Note: If the user provides `themes: {}` (an empty themes block),
		// scalityui.Spec.Themes.Light and scalityui.Spec.Themes.Dark will be zero Theme structs.
		themesData["light"] = scalityui.Spec.Themes.Light
		themesData["dark"] = scalityui.Spec.Themes.Dark
	}
	configOutput["themes"] = themesData

	return json.Marshal(configOutput)
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
func (r *ScalityUIReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appsv1.Deployment, scalityui *uiscalitycomv1alpha1.ScalityUI, configHash string) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		if err := controllerutil.SetControllerReference(scalityui, deploy, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		}

		mountPath := "/usr/share/nginx/html/shell"
		configVolumeName := scalityui.Name + "-config-volume"
		deployedAppsVolumeName := scalityui.Name + "-deployed-ui-apps-volume"
		configMapDeployedAppsName := scalityui.Name + "-deployed-ui-apps"

		zero := intstr.FromInt(0)
		one := intstr.FromInt(1)

		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &zero,
					MaxSurge:       &one,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": scalityui.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": scalityui.Name},
					Annotations: map[string]string{
						"checksum/config": configHash,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  scalityui.Name,
							Image: scalityui.Spec.Image,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      configVolumeName,
									MountPath: filepath.Join(mountPath, "config.json"),
									SubPath:   "config.json",
								},
								{
									Name:      deployedAppsVolumeName,
									MountPath: filepath.Join(mountPath, "deployed-ui-apps.json"),
									SubPath:   "deployed-ui-apps.json",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: configVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: scalityui.Name, // Name of the ConfigMap for config.json
									},
								},
							},
						},
						{
							Name: deployedAppsVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMapDeployedAppsName, // Name of the ConfigMap for deployed-ui-apps.json
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

	// Define ConfigMap for config.json
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityui.Name,
			Namespace: scalityui.Namespace,
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

	// ConfigMap for config.json is ensured to exist by createOrUpdateOwnedConfigMap

	// Define ConfigMap for deployed-ui-apps.json
	configMapDeployedAppsName := scalityui.Name + "-deployed-ui-apps"
	configMapDeployedApps := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapDeployedAppsName,
			Namespace: scalityui.Namespace,
		},
	}
	deployedAppsData := map[string]string{"deployed-ui-apps.json": "[]"}

	// Create or update ConfigMap for deployed-ui-apps.json
	configMapDeployedAppsResult, err := r.createOrUpdateOwnedConfigMap(ctx, scalityui, configMapDeployedApps, deployedAppsData)
	if err != nil {
		r.Log.Error(err, "Failed to create or update ConfigMap for deployed-ui-apps.json")
		return ctrl.Result{}, err
	}

	logOperationResult(r.Log, configMapDeployedAppsResult, "ConfigMap deployed-ui-apps.json", configMapDeployedApps.Name)

	// Verify ConfigMap for deployed-ui-apps.json exists
	err = r.Client.Get(ctx, client.ObjectKey{Name: configMapDeployedApps.Name, Namespace: configMapDeployedApps.Namespace}, &corev1.ConfigMap{})
	if err != nil {
		r.Log.Error(err, "ConfigMap for deployed-ui-apps.json was not created successfully")
		return ctrl.Result{}, err
	}
	r.Log.Info("ConfigMap deployed-ui-apps.json exists", "name", configMapDeployedApps.Name)

	// Define Deployment
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityui.Name,
			Namespace: scalityui.Namespace,
		},
	}

	// Create or update Deployment
	deploymentResult, err := r.createOrUpdateDeployment(ctx, deploy, scalityui, configHash)
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

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiscalitycomv1alpha1.ScalityUI{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
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
