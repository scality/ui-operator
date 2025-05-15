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
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// ScalityUIComponentExposerReconciler reconciles a ScalityUIComponentExposer object
type ScalityUIComponentExposerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *rest.Config
}

func (r *ScalityUIComponentExposerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling ScalityUIComponentExposer", "request", req)

	// Fetch the ScalityUIComponentExposer instance
	exposer := &uiv1alpha1.ScalityUIComponentExposer{}
	if err := r.Get(ctx, req.NamespacedName, exposer); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get ScalityUIComponentExposer")
		return ctrl.Result{}, err
	}

	// Fetch the ScalityUIComponent
	component := &uiv1alpha1.ScalityUIComponent{}
	if err := r.Get(ctx, client.ObjectKey{Name: exposer.Spec.ScalityUIComponent, Namespace: exposer.Namespace}, component); err != nil {
		if errors.IsNotFound(err) {
			// ScalityUIComponent not found - set appropriate condition
			meta.SetStatusCondition(&exposer.Status.Conditions, metav1.Condition{
				Type:               "ComponentAvailable",
				Status:             metav1.ConditionFalse,
				Reason:             "ComponentNotFound",
				Message:            fmt.Sprintf("ScalityUIComponent '%s' not found", exposer.Spec.ScalityUIComponent),
				LastTransitionTime: metav1.Now(),
			})
			if updateErr := r.Status().Update(ctx, exposer); updateErr != nil {
				logger.Error(updateErr, "Failed to update status with component not found condition")
			}
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponent")
		return ctrl.Result{}, err
	}

	// Check if the component status has the required information
	if component.Status.Kind == "" || component.Status.PublicPath == "" || component.Status.Version == "" {
		logger.Info("ScalityUIComponent status not complete yet, waiting for component controller to update",
			"component", component.Name,
			"kind", component.Status.Kind,
			"publicPath", component.Status.PublicPath,
			"version", component.Status.Version)

		meta.SetStatusCondition(&exposer.Status.Conditions, metav1.Condition{
			Type:               "ComponentConfigurationReady",
			Status:             metav1.ConditionFalse,
			Reason:             "WaitingForConfiguration",
			Message:            "Waiting for ScalityUIComponent to have complete configuration",
			LastTransitionTime: metav1.Now(),
		})

		if updateErr := r.Status().Update(ctx, exposer); updateErr != nil {
			logger.Error(updateErr, "Failed to update status with waiting condition")
		}

		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Fetch the ScalityUI
	ui := &uiv1alpha1.ScalityUI{}
	if err := r.Get(ctx, client.ObjectKey{Name: exposer.Spec.ScalityUI, Namespace: exposer.Namespace}, ui); err != nil {
		if errors.IsNotFound(err) {
			// ScalityUI not found - set appropriate condition
			meta.SetStatusCondition(&exposer.Status.Conditions, metav1.Condition{
				Type:               "UIAvailable",
				Status:             metav1.ConditionFalse,
				Reason:             "UINotFound",
				Message:            fmt.Sprintf("ScalityUI '%s' not found", exposer.Spec.ScalityUI),
				LastTransitionTime: metav1.Now(),
			})
			if updateErr := r.Status().Update(ctx, exposer); updateErr != nil {
				logger.Error(updateErr, "Failed to update status with UI not found condition")
			}
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get ScalityUI")
		return ctrl.Result{}, err
	}

	// Create ConfigMap for the runtime-app-configuration
	configMapName := fmt.Sprintf("%s-runtime-config", exposer.Name)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: exposer.Namespace,
		},
	}

	configMapData, err := createConfigMapData(component, exposer)
	if err != nil {
		logger.Error(err, "Failed to create ConfigMap data")
		return ctrl.Result{}, err
	}

	configMapResult, err := ctrl.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		// Set owner reference
		if err := ctrl.SetControllerReference(exposer, configMap, r.Scheme); err != nil {
			return err
		}

		configMap.Data = configMapData
		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update ConfigMap")
		return ctrl.Result{}, err
	}

	logger.Info("ConfigMap reconciled", "result", configMapResult)

	// Update the ScalityUIComponent deployment to mount the ConfigMap
	if err := r.updateComponentDeployment(ctx, component, configMapName, exposer.Name, logger); err != nil {
		logger.Error(err, "Failed to update ScalityUIComponent deployment")
		return ctrl.Result{}, err
	}

	// Create or update ingress if Networks is specified
	if exposer.Spec.Networks != nil {
		if err := r.reconcileIngress(ctx, exposer, component, logger); err != nil {
			logger.Error(err, "Failed to reconcile ingress")
			return ctrl.Result{}, err
		}
	}

	// Update ScalityUI deployed-ui-apps
	if err := r.updateScalityUIDeployedApps(ctx, ui, component, exposer, logger); err != nil {
		logger.Error(err, "Failed to update ScalityUI deployed-ui-apps")
		return ctrl.Result{}, err
	}

	// Update status with component information
	exposer.Status.Kind = component.Status.Kind
	exposer.Status.PublicPath = component.Status.PublicPath
	exposer.Status.Version = component.Status.Version

	// Update success conditions
	meta.SetStatusCondition(&exposer.Status.Conditions, metav1.Condition{
		Type:               "ConfigMapCreated",
		Status:             metav1.ConditionTrue,
		Reason:             "Created",
		Message:            "Runtime configuration ConfigMap created successfully",
		LastTransitionTime: metav1.Now(),
	})

	meta.SetStatusCondition(&exposer.Status.Conditions, metav1.Condition{
		Type:               "ComponentConfigurationReady",
		Status:             metav1.ConditionTrue,
		Reason:             "ConfigurationAvailable",
		Message:            "ScalityUIComponent configuration is available",
		LastTransitionTime: metav1.Now(),
	})

	// Update the status
	if err := r.Status().Update(ctx, exposer); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	logger.Info("ScalityUIComponentExposer status updated with configuration",
		"kind", exposer.Status.Kind,
		"publicPath", exposer.Status.PublicPath,
		"version", exposer.Status.Version)

	return ctrl.Result{}, nil
}

// createConfigMapData creates the data for the runtime-app-configuration ConfigMap
func createConfigMapData(component *uiv1alpha1.ScalityUIComponent, exposer *uiv1alpha1.ScalityUIComponentExposer) (map[string]string, error) {
	// Create runtime configuration data based on component status and exposer spec
	runtimeConfig := map[string]interface{}{
		"kind":       component.Status.Kind,
		"publicPath": component.Status.PublicPath,
		"version":    component.Status.Version,
	}

	// Add selfConfiguration if provided in the exposer
	if exposer.Spec.RuntimeAppConfiguration != nil && exposer.Spec.RuntimeAppConfiguration.SelfConfiguration.URL != "" {
		runtimeConfig["url"] = exposer.Spec.RuntimeAppConfiguration.SelfConfiguration.URL
	}

	// Add auth configuration if provided
	if exposer.Spec.Auth != nil {
		authConfig := map[string]interface{}{
			"kind":           exposer.Spec.Auth.Kind,
			"providerUrl":    exposer.Spec.Auth.ProviderURL,
			"redirectUrl":    exposer.Spec.Auth.RedirectURL,
			"clientId":       exposer.Spec.Auth.ClientID,
			"responseType":   exposer.Spec.Auth.ResponseType,
			"scopes":         exposer.Spec.Auth.Scopes,
			"providerLogout": exposer.Spec.Auth.ProviderLogout,
		}
		runtimeConfig["auth"] = authConfig
	}

	// Convert to JSON
	jsonData, err := json.Marshal(runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal runtime configuration: %w", err)
	}

	return map[string]string{
		"runtime-app-configuration.json": string(jsonData),
	}, nil
}

// updateComponentDeployment updates the ScalityUIComponent deployment to mount the ConfigMap
func (r *ScalityUIComponentExposerReconciler) updateComponentDeployment(ctx context.Context, component *uiv1alpha1.ScalityUIComponent, configMapName, exposerName string, logger logr.Logger) error {
	// Fetch the deployment
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: component.Name, Namespace: component.Namespace}, deployment); err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Update the deployment to mount the ConfigMap
	deploymentChanged := false

	// Check if volume already exists
	volumeName := fmt.Sprintf("%s-config-volume", exposerName)
	volumeExists := false
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == volumeName {
			volumeExists = true
			// Check if ConfigMap name is correct
			if volume.ConfigMap == nil || volume.ConfigMap.Name != configMapName {
				// Update ConfigMap name
				volume.ConfigMap.Name = configMapName
				deploymentChanged = true
			}
			break
		}
	}

	// Add volume if it doesn't exist
	if !volumeExists {
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
		deploymentChanged = true
	}

	// Check if volume mount already exists in containers
	volumeMountPath := "/usr/share/nginx/html/.well-known/runtime-app-configuration.json"
	subPath := "runtime-app-configuration.json"

	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		volumeMountExists := false

		for j, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == volumeName {
				volumeMountExists = true
				// Check if mount path and subPath are correct
				if volumeMount.MountPath != volumeMountPath || volumeMount.SubPath != subPath {
					// Update mount path and subPath
					container.VolumeMounts[j].MountPath = volumeMountPath
					container.VolumeMounts[j].SubPath = subPath
					deploymentChanged = true
				}
				break
			}
		}

		// Add volume mount if it doesn't exist
		if !volumeMountExists {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: volumeMountPath,
				SubPath:   subPath,
			})
			deploymentChanged = true
		}
	}

	// Update deployment if changes were made
	if deploymentChanged {
		if err := r.Update(ctx, deployment); err != nil {
			return fmt.Errorf("failed to update deployment: %w", err)
		}
		logger.Info("Deployment updated with ConfigMap volume",
			"deployment", deployment.Name,
			"configMap", configMapName)
	}

	return nil
}

// reconcileIngress creates or updates an ingress for the ScalityUIComponentExposer
func (r *ScalityUIComponentExposerReconciler) reconcileIngress(ctx context.Context, exposer *uiv1alpha1.ScalityUIComponentExposer, component *uiv1alpha1.ScalityUIComponent, logger logr.Logger) error {
	// Create Ingress name based on exposer name
	ingressName := fmt.Sprintf("%s-ingress", exposer.Name)

	// Create Ingress resource
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: exposer.Namespace,
		},
	}

	// Path for the ingress rule
	path := component.Status.PublicPath
	if path == "" {
		path = "/"
	}

	// Create or update the ingress
	ingressResult, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		// Set owner reference
		if err := ctrl.SetControllerReference(exposer, ingress, r.Scheme); err != nil {
			return err
		}

		// Set annotations if provided
		if exposer.Spec.Networks.IngressAnnotations != nil {
			if ingress.ObjectMeta.Annotations == nil {
				ingress.ObjectMeta.Annotations = make(map[string]string)
			}

			for key, value := range exposer.Spec.Networks.IngressAnnotations {
				ingress.ObjectMeta.Annotations[key] = value
			}
		}

		// Configure TLS if provided
		if len(exposer.Spec.Networks.TLS) > 0 {
			ingress.Spec.TLS = []networkingv1.IngressTLS{}

			for _, tlsConfig := range exposer.Spec.Networks.TLS {
				ingress.Spec.TLS = append(ingress.Spec.TLS, networkingv1.IngressTLS{
					Hosts:      tlsConfig.Hosts,
					SecretName: tlsConfig.SecretName,
				})
			}
		}

		// Configure rules
		pathType := networkingv1.PathTypePrefix
		ingress.Spec.Rules = []networkingv1.IngressRule{
			{
				Host: exposer.Spec.Networks.Host,
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
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update ingress: %w", err)
	}

	logger.Info("Ingress reconciled", "result", ingressResult)
	return nil
}

// updateScalityUIDeployedApps updates the ScalityUI deployed-ui-apps ConfigMap
func (r *ScalityUIComponentExposerReconciler) updateScalityUIDeployedApps(ctx context.Context, ui *uiv1alpha1.ScalityUI, component *uiv1alpha1.ScalityUIComponent, exposer *uiv1alpha1.ScalityUIComponentExposer, logger logr.Logger) error {
	// Get the current deployed-ui-apps ConfigMap
	deployedAppsConfigMapName := fmt.Sprintf("%s-deployed-ui-apps", ui.Name)
	deployedAppsConfigMap := &corev1.ConfigMap{}

	err := r.Get(ctx, types.NamespacedName{Name: deployedAppsConfigMapName, Namespace: ui.Namespace}, deployedAppsConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create a new ConfigMap if it doesn't exist
			deployedAppsConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployedAppsConfigMapName,
					Namespace: ui.Namespace,
				},
				Data: map[string]string{},
			}

			// Set owner reference to the ScalityUI
			if err := ctrl.SetControllerReference(ui, deployedAppsConfigMap, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on deployed-ui-apps ConfigMap: %w", err)
			}
		} else {
			return fmt.Errorf("failed to get deployed-ui-apps ConfigMap: %w", err)
		}
	}

	// Create or update ConfigMap
	configMapResult, err := ctrl.CreateOrUpdate(ctx, r.Client, deployedAppsConfigMap, func() error {
		// Initialize data if nil
		if deployedAppsConfigMap.Data == nil {
			deployedAppsConfigMap.Data = make(map[string]string)
		}

		// Create app configuration for this component
		appConfig := map[string]interface{}{
			"kind":       component.Status.Kind,
			"publicPath": component.Status.PublicPath,
			"version":    component.Status.Version,
		}

		// Add app history base path if provided
		if exposer.Spec.AppHistoryBasePath != "" {
			appConfig["appHistoryBasePath"] = exposer.Spec.AppHistoryBasePath
		}

		// Convert to JSON
		appConfigJSON, err := json.Marshal(appConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal app configuration: %w", err)
		}

		// Add to ConfigMap data with key based on component name
		deployedAppsConfigMap.Data[component.Name] = string(appConfigJSON)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update deployed-ui-apps ConfigMap: %w", err)
	}

	logger.Info("Deployed-ui-apps ConfigMap reconciled", "result", configMapResult)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
