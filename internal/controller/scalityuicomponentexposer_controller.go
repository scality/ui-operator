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

const configMapKey = "runtime-app-configuration"
const configHashAnnotation = "ui.scality.com/config-hash"
const volumeNamePrefix = "config-volume-"

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ScalityUIComponentExposerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ScalityUIComponentExposer instance
	exposer := &uiv1alpha1.ScalityUIComponentExposer{}
	if err := r.Get(ctx, req.NamespacedName, exposer); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponentExposer")
		return ctrl.Result{}, err
	}

	// Fetch the ScalityUIComponent
	component := &uiv1alpha1.ScalityUIComponent{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      exposer.Spec.ScalityUIComponent,
		Namespace: exposer.Namespace,
	}, component); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get ScalityUIComponent", "componentName", exposer.Spec.ScalityUIComponent)
		return ctrl.Result{}, err
	}

	// Fetch the ScalityUI
	ui := &uiv1alpha1.ScalityUI{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      exposer.Spec.ScalityUI,
		Namespace: exposer.Namespace,
	}, ui); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get ScalityUI", "uiName", exposer.Spec.ScalityUI)
		return ctrl.Result{}, err
	}

	// Create or update the Ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ingress", exposer.Name),
			Namespace: exposer.Namespace,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		if err := ctrl.SetControllerReference(exposer, ingress, r.Scheme); err != nil {
			return err
		}
		if ingress.Labels == nil {
			ingress.Labels = make(map[string]string)
		}
		ingress.Labels["app.kubernetes.io/name"] = exposer.Name
		ingress.Labels["app.kubernetes.io/part-of"] = ui.Spec.ProductName
		ingress.Labels["app.kubernetes.io/component"] = component.Name

		path := fmt.Sprintf("/%s", component.Name)
		pathType := networkingv1.PathTypePrefix
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
		logger.Error(err, "Failed to create or update Ingress")
		return ctrl.Result{}, err
	}

	// Create or update ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      exposer.Name,
			Namespace: exposer.Namespace,
		},
	}

	_, err = ctrl.CreateOrUpdate(ctx, r.Client, configMap, func() error {
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
		logger.Error(err, "Failed to create or update ConfigMap")
		return ctrl.Result{}, err
	}

	// Calculate ConfigMap hash
	dataToHash, dataKeyExists := configMap.Data[configMapKey]
	if !dataKeyExists {
		logger.Error(fmt.Errorf("key '%s' missing from ConfigMap '%s'", configMapKey, configMap.Name), "ConfigMap data missing")
		return ctrl.Result{RequeueAfter: time.Minute}, fmt.Errorf("key %s missing from configmap %s", configMapKey, configMap.Name)
	}
	hash := sha256.Sum256([]byte(dataToHash))
	configMapHash := hex.EncodeToString(hash[:])

	// Update component's deployment to mount ConfigMap
	deployment := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      component.Name,
		Namespace: exposer.Namespace,
	}, deployment)

	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		logger.Error(err, "Failed to get component deployment")
		return ctrl.Result{}, err
	}

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
		logger.Error(err, "Failed to update deployment with ConfigMap mount")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentExposerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponentExposer{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
