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
	"io"
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// DefaultServicePort is the default port used to connect to the UI component service
const DefaultServicePort = 80

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

// ConfigFetcher defines an interface for fetching UI component configurations
type ConfigFetcher interface {
	FetchConfig(ctx context.Context, namespace, serviceName string, port int) (string, error)
}

// K8sServiceProxyFetcher implements ConfigFetcher using Kubernetes service proxy
type K8sServiceProxyFetcher struct {
	// Config *rest.Config // No longer needed for direct HTTP GET
}

// FetchConfig retrieves the micro-app configuration from the specified service
func (f *K8sServiceProxyFetcher) FetchConfig(ctx context.Context, namespace, serviceName string, port int) (string, error) {
	logger := log.FromContext(ctx)

	// Construct the in-cluster service URL
	// Example: http://my-service.my-namespace.svc.cluster.local:80/.well-known/micro-app-configuration
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/.well-known/micro-app-configuration", serviceName, namespace, port)

	logger.Info("Fetching configuration via direct HTTP GET", "url", url)

	httpClient := &http.Client{
		Timeout: 10 * time.Second, // Add a timeout for the HTTP request
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Error(err, "Failed to create HTTP request")
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Error(err, "Failed to get configuration via direct HTTP GET")
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("failed to fetch configuration, status code: %d", resp.StatusCode)
		logger.Error(err, "HTTP request failed")
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err, "Failed to read response body")
		return "", err
	}

	return string(body), nil
}

type ScalityUIComponentReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ConfigFetcher ConfigFetcher
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/finalizers,verbs=update

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
		// Set the ScalityUIComponent as the owner of this Deployment
		if err := controllerutil.SetControllerReference(scalityUIComponent, deployment, r.Scheme); err != nil {
			return err
		}
		// Preserve existing volumes and annotations if they exist
		existingVolumes := deployment.Spec.Template.Spec.Volumes
		existingAnnotations := deployment.Spec.Template.Annotations

		// Preserve existing volume mounts for each container
		var existingVolumeMounts [][]corev1.VolumeMount
		if len(deployment.Spec.Template.Spec.Containers) > 0 {
			existingVolumeMounts = make([][]corev1.VolumeMount, len(deployment.Spec.Template.Spec.Containers))
			for i, container := range deployment.Spec.Template.Spec.Containers {
				existingVolumeMounts[i] = container.VolumeMounts
			}
		}
		deployment.Labels["app"] = scalityUIComponent.Name

		// Set selector if it doesn't exist
		if deployment.Spec.Selector == nil {
			deployment.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": scalityUIComponent.Name,
				},
			}
		}

		if deployment.Spec.Template.Labels == nil {
			deployment.Spec.Template.Labels = make(map[string]string)
		}
		deployment.Spec.Template.Labels["app"] = scalityUIComponent.Name

		// Preserve existing annotations while allowing new ones
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}

		// Ensure we have at least one container with the correct image
		containerFound := false
		for i := range deployment.Spec.Template.Spec.Containers {
			if deployment.Spec.Template.Spec.Containers[i].Name == scalityUIComponent.Name {
				// Update the image for the existing container
				deployment.Spec.Template.Spec.Containers[i].Image = scalityUIComponent.Spec.Image
				containerFound = true
				break
			}
		}

		// If container doesn't exist, add it
		if !containerFound {
			deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, corev1.Container{
				Name:  scalityUIComponent.Name,
				Image: scalityUIComponent.Spec.Image,
			})
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
		// Set the ScalityUIComponent as the owner of this Service
		if err := controllerutil.SetControllerReference(scalityUIComponent, service, r.Scheme); err != nil {
			return err
		}

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

	// Re-fetch the Deployment to get its latest status, particularly ReadyReplicas
	// This is crucial for determining if pods are ready before attempting to fetch the UI configuration
	if err := r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, deployment); err != nil {
		logger.Error(err, "Failed to get deployment status")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	if deployment.Status.ReadyReplicas > 0 {
		// Fetch and process configuration
		return r.processUIComponentConfig(ctx, scalityUIComponent)
	} else {
		logger.Info("Deployment not ready yet, waiting for pods to start")
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}
}

// processUIComponentConfig fetches and processes UI component configuration,
// updates the status and returns the reconcile result
func (r *ScalityUIComponentReconciler) processUIComponentConfig(ctx context.Context, scalityUIComponent *uiv1alpha1.ScalityUIComponent) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch configuration
	configContent, err := r.fetchMicroAppConfig(ctx, scalityUIComponent.Namespace, scalityUIComponent.Name)

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

	// Parse and apply configuration
	result, err := r.parseAndApplyConfig(ctx, scalityUIComponent, configContent)
	if err != nil {
		return result, nil // Error handling is done in parseAndApplyConfig
	}

	logger.Info("ScalityUIComponent status updated with configuration",
		"kind", scalityUIComponent.Status.Kind,
		"publicPath", scalityUIComponent.Status.PublicPath,
		"version", scalityUIComponent.Status.Version)

	return ctrl.Result{}, nil
}

// parseAndApplyConfig parses the configuration content and updates the ScalityUIComponent status
func (r *ScalityUIComponentReconciler) parseAndApplyConfig(ctx context.Context,
	scalityUIComponent *uiv1alpha1.ScalityUIComponent, configContent string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

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

		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}

	// Update status with retrieved information
	scalityUIComponent.Status.Kind = config.Metadata.Kind
	scalityUIComponent.Status.PublicPath = config.Spec.PublicPath
	scalityUIComponent.Status.Version = config.Spec.Version

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

	return ctrl.Result{}, nil
}

func (r *ScalityUIComponentReconciler) fetchMicroAppConfig(ctx context.Context, namespace, serviceName string) (string, error) {
	// Use the ConfigFetcher if available
	if r.ConfigFetcher != nil {
		return r.ConfigFetcher.FetchConfig(ctx, namespace, serviceName, DefaultServicePort)
	}

	// This fallback should ideally not be reached if ConfigFetcher is always initialized in SetupWithManager.
	// However, as a safeguard or for direct calls in tests without full manager setup:
	// We need a rest.Config here. The reconciler should have one if properly set up.
	// This path indicates a potential misconfiguration or misuse if r.Config is nil.
	logger := log.FromContext(ctx)
	logger.Error(fmt.Errorf("ConfigFetcher not provided and no default REST config available in reconciler"), "Cannot fetch config without a ConfigFetcher or REST config")
	return "", fmt.Errorf("config fetcher not initialized and no default REST config")
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// config, err := rest.InClusterConfig() // No longer needed here if K8sServiceProxyFetcher doesn't need it
	// if err != nil {
	// 	log.FromContext(context.Background()).Info("Failed to get in-cluster config, using manager's config as fallback", "error", err.Error())
	// 	config = mgr.GetConfig()
	// }

	// Initialize the default config fetcher if not explicitly provided
	if r.ConfigFetcher == nil {
		r.ConfigFetcher = &K8sServiceProxyFetcher{} // Initialize without config
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
