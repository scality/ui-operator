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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// MicroAppConfig represents the structure of the micro-app-configuration file
type MicroAppConfig struct {
	Kind       string     `json:"kind"`
	ApiVersion string     `json:"apiVersion"`
	Metadata   ConfigMeta `json:"metadata"`
	Spec       ConfigSpec `json:"spec"`
}

// ConfigMeta represents the metadata section of the micro-app-configuration
type ConfigMeta struct {
	Kind string `json:"kind"`
}

// ConfigSpec represents the spec section of the micro-app-configuration
type ConfigSpec struct {
	RemoteEntryPath string `json:"remoteEntryPath"`
	PublicPath      string `json:"publicPath"`
	Version         string `json:"version"`
}

// FetchConfigFunc is a function type for configuration retrieval
type FetchConfigFunc func(ctx context.Context, namespace, serviceName string) (string, error)

// ExposerCreatorFunc is a function type for creating or updating exposers
type ExposerCreatorFunc func(ctx context.Context, component *uiv1alpha1.ScalityUIComponent) error

type ScalityUIComponentReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Config          *rest.Config
	fetchConfigFunc FetchConfigFunc
	// Allow injection of a custom exposer creator function for testing
	createOrUpdateExposerFunc ExposerCreatorFunc
}

func (r *ScalityUIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	scalityUIComponent := &uiv1alpha1.ScalityUIComponent{}
	if err := r.Get(ctx, req.NamespacedName, scalityUIComponent); err != nil {
		logger.Error(err, "Failed to get ScalityUIComponent")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityUIComponent.Name,
			Namespace: scalityUIComponent.Namespace,
		},
	}

	deploymentResult, err := ctrl.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		deployment.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": scalityUIComponent.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": scalityUIComponent.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  scalityUIComponent.Name,
							Image: scalityUIComponent.Spec.Image,
						},
					},
				},
			},
		}
		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update Deployment")
		return ctrl.Result{}, err
	}

	logger.Info("Deployment reconciled", "result", deploymentResult)

	// Service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scalityUIComponent.Name,
			Namespace: scalityUIComponent.Namespace,
		},
	}

	serviceResult, err := ctrl.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec = corev1.ServiceSpec{
			Selector: map[string]string{
				"app": scalityUIComponent.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		}
		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update Service")
		return ctrl.Result{}, err
	}

	logger.Info("Service reconciled", "result", serviceResult)

	if err := r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, deployment); err != nil {
		logger.Error(err, "Failed to get deployment status")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	if deployment.Status.ReadyReplicas > 0 {
		// Use the injected function to retrieve configuration or the default function
		fetchConfig := r.fetchConfigFunc
		if fetchConfig == nil {
			fetchConfig = r.fetchMicroAppConfig
		}

		configContent, err := fetchConfig(ctx, scalityUIComponent.Namespace, scalityUIComponent.Name)

		if err != nil {
			logger.Error(err, "Failed to fetch micro-app-configuration")

			// Add a failure condition
			meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
				Type:               "ConfigurationRetrieved",
				Status:             metav1.ConditionFalse,
				Reason:             "FetchFailed",
				Message:            fmt.Sprintf("Failed to fetch configuration: %v", err),
				LastTransitionTime: metav1.Now(),
			})

			if updateErr := r.Status().Update(ctx, scalityUIComponent); updateErr != nil {
				logger.Error(updateErr, "Failed to update status with failure condition")
			}

			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

		// Parse JSON to extract information
		var config MicroAppConfig
		if err := json.Unmarshal([]byte(configContent), &config); err != nil {
			logger.Error(err, "Failed to parse micro-app-configuration")

			// Add a failure condition
			meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
				Type:               "ConfigurationRetrieved",
				Status:             metav1.ConditionFalse,
				Reason:             "ParseFailed",
				Message:            fmt.Sprintf("Failed to parse configuration: %v", err),
				LastTransitionTime: metav1.Now(),
			})

			if updateErr := r.Status().Update(ctx, scalityUIComponent); updateErr != nil {
				logger.Error(updateErr, "Failed to update status with failure condition")
			}

			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}

		// Update status with retrieved information
		scalityUIComponent.Status.Kind = config.Metadata.Kind
		scalityUIComponent.Status.PublicPath = config.Spec.PublicPath
		scalityUIComponent.Status.Version = config.Spec.Version

		logger.Info("Debug config values",
			"metadata.kind", config.Metadata.Kind,
			"spec.publicPath", config.Spec.PublicPath,
			"spec.version", config.Spec.Version)

		// Add a success condition
		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:               "ConfigurationRetrieved",
			Status:             metav1.ConditionTrue,
			Reason:             "FetchSucceeded",
			Message:            "Successfully fetched and applied UI component configuration",
			LastTransitionTime: metav1.Now(),
		})

		// Update the status
		if err := r.Status().Update(ctx, scalityUIComponent); err != nil {
			logger.Error(err, "Failed to update status with configuration")
			return ctrl.Result{}, err
		}

		logger.Info("ScalityUIComponent status updated with configuration",
			"kind", scalityUIComponent.Status.Kind,
			"publicPath", scalityUIComponent.Status.PublicPath,
			"version", scalityUIComponent.Status.Version)

		// Create or update a ScalityUIComponentExposer for this component
		if r.createOrUpdateExposerFunc != nil {
			// Use injected function if provided (for testing)
			if err := r.createOrUpdateExposerFunc(ctx, scalityUIComponent); err != nil {
				logger.Error(err, "Failed to create or update ScalityUIComponentExposer")
				return ctrl.Result{}, err
			}
		} else {
			// Use default implementation
			if err := r.createOrUpdateExposer(ctx, scalityUIComponent); err != nil {
				logger.Error(err, "Failed to create or update ScalityUIComponentExposer")
				return ctrl.Result{}, err
			}
		}
	} else {
		logger.Info("Deployment not ready yet, waiting for pods to start")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ScalityUIComponentReconciler) fetchMicroAppConfig(ctx context.Context, namespace, serviceName string) (string, error) {
	clientset, err := kubernetes.NewForConfig(r.Config)
	if err != nil {
		return "", fmt.Errorf("failed to create clientset: %w", err)
	}

	restClient := clientset.CoreV1().RESTClient()
	req := restClient.Get().
		Namespace(namespace).
		Resource("services").
		Name(fmt.Sprintf("%s:80", serviceName)).
		SubResource("proxy").
		Suffix("/.well-known/micro-app-configuration")

	// Execute the request
	result := req.Do(ctx)
	raw, err := result.Raw()
	if err != nil {
		return "", fmt.Errorf("failed to get configuration: %w", err)
	}

	return string(raw), nil
}

func (r *ScalityUIComponentReconciler) createOrUpdateExposer(ctx context.Context, component *uiv1alpha1.ScalityUIComponent) error {
	logger := log.FromContext(ctx)

	// Find an available ScalityUI in the same namespace
	// TODO could be enhanced to select based on labels or annotations
	scalityUIList := &uiv1alpha1.ScalityUIList{}
	if err := r.List(ctx, scalityUIList, client.InNamespace(component.Namespace)); err != nil {
		return fmt.Errorf("failed to list ScalityUI resources: %w", err)
	}

	if len(scalityUIList.Items) == 0 {
		return fmt.Errorf("no ScalityUI found in namespace %s to expose component", component.Namespace)
	}

	// Get the first ScalityUI
	scalityUI := scalityUIList.Items[0]

	// Create exposer name based on component name
	exposerName := component.Name + "-exposer"

	// Check if exposer already exists
	exposer := &uiv1alpha1.ScalityUIComponentExposer{}
	err := r.Get(ctx, client.ObjectKey{Name: exposerName, Namespace: component.Namespace}, exposer)

	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing ScalityUIComponentExposer: %w", err)
	}

	// If exposer doesn't exist, create a new one
	if errors.IsNotFound(err) {
		logger.Info("Creating new ScalityUIComponentExposer", "name", exposerName)

		// Ensure we have valid APIVersion and Kind
		apiVersion := "ui.scality.com/v1alpha1"
		if component.APIVersion != "" {
			apiVersion = component.APIVersion
		}

		kind := "ScalityUIComponent"
		if component.Kind != "" {
			kind = component.Kind
		}

		exposer = &uiv1alpha1.ScalityUIComponentExposer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      exposerName,
				Namespace: component.Namespace,
				// Add owner reference to the component
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: apiVersion,
						Kind:       kind,
						Name:       component.Name,
						UID:        component.UID,
						Controller: pointer(true),
					},
				},
			},
			Spec: uiv1alpha1.ScalityUIComponentExposerSpec{
				ScalityUI:          scalityUI.Name,
				ScalityUIComponent: component.Name,
			},
		}

		if err = r.Create(ctx, exposer); err != nil {
			return fmt.Errorf("failed to create ScalityUIComponentExposer: %w", err)
		}

		logger.Info("Successfully created ScalityUIComponentExposer",
			"name", exposerName,
			"component", component.Name,
			"ui", scalityUI.Name)
	} else {
		// Exposer exists, update if necessary
		logger.Info("ScalityUIComponentExposer already exists", "name", exposerName)

		// Update fields if needed
		updated := false

		if exposer.Spec.ScalityUI != scalityUI.Name {
			exposer.Spec.ScalityUI = scalityUI.Name
			updated = true
		}

		if exposer.Spec.ScalityUIComponent != component.Name {
			exposer.Spec.ScalityUIComponent = component.Name
			updated = true
		}

		if updated {
			if err = r.Update(ctx, exposer); err != nil {
				return fmt.Errorf("failed to update ScalityUIComponentExposer: %w", err)
			}
			logger.Info("Successfully updated ScalityUIComponentExposer", "name", exposerName)
		}
	}

	return nil
}

// helper function to create a pointer to a boolean
func pointer(b bool) *bool {
	return &b
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Store the configuration for later use
	r.Config = mgr.GetConfig()

	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
