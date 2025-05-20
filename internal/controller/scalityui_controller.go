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
	DefaultScalityUINamespace = "default"
)

// createConfigJSON creates a JSON config from the ScalityUI object
func createConfigJSON(scalityui *uiscalitycomv1alpha1.ScalityUI) ([]byte, error) {
	configJSON := map[string]string{
		"productName": scalityui.Spec.ProductName,
	}

	return json.Marshal(configJSON)
}

// createOrUpdateConfigMap creates or updates a ConfigMap with the given configuration
func (r *ScalityUIReconciler) createOrUpdateConfigMap(ctx context.Context, configMap *corev1.ConfigMap, configJSON []byte) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Data = map[string]string{
			"config.json": string(configJSON),
		}
		return nil
	})
}

// createOrUpdateDeployment creates or updates a Deployment with the given configuration
func (r *ScalityUIReconciler) createOrUpdateDeployment(ctx context.Context, deploy *appsv1.Deployment, scalityui *uiscalitycomv1alpha1.ScalityUI, configHash string) (controllerutil.OperationResult, error) {
	return controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {

		mountPath := "/usr/share/nginx/html/shell"
		if scalityui.Spec.MountPath != "" {
			mountPath = scalityui.Spec.MountPath
		}

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
						{Name: scalityui.Name, Image: scalityui.Spec.Image, VolumeMounts: []corev1.VolumeMount{
							{
								Name:      scalityui.Name + "-volume",
								MountPath: filepath.Join(mountPath, "config.json"),
								SubPath:   "config.json",
							},
						}},
					},
					Volumes: []corev1.Volume{
						{
							Name: scalityui.Name + "-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: scalityui.Name,
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

	// Define ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityui.Name,
			Namespace: scalityui.Namespace,
		},
		Data: map[string]string{
			"config.json": string(configJSON),
		},
	}

	// Create or update ConfigMap
	configMapResult, err := r.createOrUpdateConfigMap(ctx, configMap, configJSON)
	if err != nil {
		r.Log.Error(err, "Failed to create or update ConfigMap")
		return ctrl.Result{}, err
	}

	// Log ConfigMap operation result
	switch configMapResult {
	case controllerutil.OperationResultCreated:
		r.Log.Info("ConfigMap created", "name", configMap.Name)
	case controllerutil.OperationResultUpdated:
		r.Log.Info("ConfigMap updated", "name", configMap.Name)
	}

	// Verify ConfigMap exists
	existingConfigMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: configMap.Name, Namespace: configMap.Namespace}, existingConfigMap)
	if err != nil {
		r.Log.Error(err, "ConfigMap was not created successfully")
		return ctrl.Result{}, err
	}
	r.Log.Info("ConfigMap exists", "name", existingConfigMap.Name)

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
		Complete(r)
}
