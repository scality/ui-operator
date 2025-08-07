package scalityuicomponent

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
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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
	logger := ctrl.LoggerFrom(ctx)

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
		logger.Error(err, "Failed to fetch configuration")
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err, "Failed to read response body")
		return "", err
	}

	logger.Info("Successfully fetched configuration", "length", len(body))
	return string(body), nil
}

// ScalityUIComponentReconciler reconciles a ScalityUIComponent object
type ScalityUIComponentReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ConfigFetcher ConfigFetcher
}

// NewScalityUIComponentReconciler creates a new ScalityUIComponentReconciler
func NewScalityUIComponentReconciler(client client.Client, scheme *runtime.Scheme) *ScalityUIComponentReconciler {
	return &ScalityUIComponentReconciler{
		Client:        client,
		Scheme:        scheme,
		ConfigFetcher: &K8sServiceProxyFetcher{},
	}
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/finalizers,verbs=update

func (r *ScalityUIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

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

		// Use imagePullSecrets directly
		imagePullSecrets := append([]corev1.LocalObjectReference{}, scalityUIComponent.Spec.ImagePullSecrets...)

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
					Annotations: existingAnnotations,
				},
				Spec: corev1.PodSpec{
					Volumes:          existingVolumes,
					ImagePullSecrets: imagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:  scalityUIComponent.Name,
							Image: scalityUIComponent.Spec.Image,
						},
					},
				},
			},
		}

		// Restore volume mounts for all containers if they existed
		if len(existingVolumeMounts) > 0 {
			for i := 0; i < len(existingVolumeMounts) && i < len(deployment.Spec.Template.Spec.Containers); i++ {
				if len(existingVolumeMounts[i]) > 0 {
					deployment.Spec.Template.Spec.Containers[i].VolumeMounts = existingVolumeMounts[i]
				}
			}
		}

		// Apply tolerations from CR spec
		if scalityUIComponent.Spec.Scheduling != nil && len(scalityUIComponent.Spec.Scheduling.Tolerations) > 0 {
			deployment.Spec.Template.Spec.Tolerations = scalityUIComponent.Spec.Scheduling.Tolerations
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
					Name:       "http",
					Port:       DefaultServicePort,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(DefaultServicePort),
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

	// Process UI component configuration
	return r.processUIComponentConfig(ctx, scalityUIComponent)
}

func (r *ScalityUIComponentReconciler) processUIComponentConfig(ctx context.Context, scalityUIComponent *uiv1alpha1.ScalityUIComponent) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	// Check if the service is ready
	service := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: scalityUIComponent.Name, Namespace: scalityUIComponent.Namespace}, service)
	if err != nil {
		logger.Info("Service not ready yet, will retry", "error", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Check if the deployment is ready
	deployment := &appsv1.Deployment{}
	err = r.Get(ctx, client.ObjectKey{Name: scalityUIComponent.Name, Namespace: scalityUIComponent.Namespace}, deployment)
	if err != nil {
		logger.Info("Deployment not found yet, will retry", "error", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Check if deployment has ready replicas
	if deployment.Status.ReadyReplicas == 0 {
		logger.Info("Deployment not ready yet (0 ready replicas), will retry")
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if configuration was already retrieved successfully
	existingCond := meta.FindStatusCondition(scalityUIComponent.Status.Conditions, "ConfigurationRetrieved")
	if existingCond != nil && existingCond.Status == metav1.ConditionTrue {
		logger.Info("Configuration already retrieved successfully, skipping fetch")
		return ctrl.Result{}, nil
	}

	// Fetch micro-app configuration
	configContent, err := r.fetchMicroAppConfig(ctx, scalityUIComponent.Namespace, scalityUIComponent.Name)
	if err != nil {
		logger.Error(err, "Failed to fetch micro-app configuration")

		// Set ConfigurationRetrieved=False condition
		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:    "ConfigurationRetrieved",
			Status:  metav1.ConditionFalse,
			Reason:  "FetchFailed",
			Message: fmt.Sprintf("Failed to fetch configuration: %v", err),
		})

		// Update the status
		if statusErr := r.Status().Update(ctx, scalityUIComponent); statusErr != nil {
			logger.Error(statusErr, "Failed to update ScalityUIComponent status after fetch failure")
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// Parse and apply configuration
	return r.parseAndApplyConfig(ctx, scalityUIComponent, configContent)
}

func (r *ScalityUIComponentReconciler) parseAndApplyConfig(ctx context.Context,
	scalityUIComponent *uiv1alpha1.ScalityUIComponent, configContent string) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var config MicroAppConfig
	if err := json.Unmarshal([]byte(configContent), &config); err != nil {
		logger.Error(err, "Failed to parse micro-app configuration")

		// Set ConfigurationRetrieved=False condition with ParseFailed reason
		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:    "ConfigurationRetrieved",
			Status:  metav1.ConditionFalse,
			Reason:  "ParseFailed",
			Message: fmt.Sprintf("Failed to parse configuration: %v", err),
		})

		// Update the status even on parse failure
		if statusErr := r.Status().Update(ctx, scalityUIComponent); statusErr != nil {
			logger.Error(statusErr, "Failed to update ScalityUIComponent status after parse failure")
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// Update status with configuration details
	scalityUIComponent.Status.Kind = config.Metadata.Kind
	scalityUIComponent.Status.PublicPath = config.Spec.PublicPath
	scalityUIComponent.Status.Version = config.Spec.Version

	// Set ConfigurationRetrieved=True condition
	meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
		Type:    "ConfigurationRetrieved",
		Status:  metav1.ConditionTrue,
		Reason:  "FetchSucceeded",
		Message: "Successfully fetched and applied UI component configuration",
	})

	// Update the status
	if err := r.Status().Update(ctx, scalityUIComponent); err != nil {
		logger.Error(err, "Failed to update ScalityUIComponent status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully updated ScalityUIComponent status",
		"kind", config.Metadata.Kind,
		"publicPath", config.Spec.PublicPath,
		"version", config.Spec.Version)

	return ctrl.Result{}, nil
}

func (r *ScalityUIComponentReconciler) fetchMicroAppConfig(ctx context.Context, namespace, serviceName string) (string, error) {
	return r.ConfigFetcher.FetchConfig(ctx, namespace, serviceName, DefaultServicePort)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiv1alpha1.ScalityUIComponent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
