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
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

// ConfigFetcher defines an interface for fetching UI component configurations
type ConfigFetcher interface {
	FetchConfig(ctx context.Context, namespace, serviceName string, port int) (string, error)
}

// K8sServiceProxyFetcher implements ConfigFetcher using Kubernetes service proxy
type K8sServiceProxyFetcher struct {
	// Config *rest.Config // No longer needed for direct HTTP GET
}

// validateMicroAppConfig validates the fetched micro-app configuration
func validateMicroAppConfig(config *MicroAppConfig) error {
	if config.Metadata.Kind == "" {
		return fmt.Errorf("configuration missing required field: metadata.kind")
	}
	if config.Spec.PublicPath == "" {
		return fmt.Errorf("configuration missing required field: spec.publicPath")
	}
	if config.Spec.Version == "" {
		return fmt.Errorf("configuration missing required field: spec.version")
	}
	return nil
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

		// Only set Selector on creation (it's immutable)
		if deployment.Spec.Selector == nil {
			deployment.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": scalityUIComponent.Name,
				},
			}
		}

		// Update pod template labels
		if deployment.Spec.Template.Labels == nil {
			deployment.Spec.Template.Labels = make(map[string]string)
		}
		deployment.Spec.Template.Labels["app"] = scalityUIComponent.Name

		// Update container image (find existing or create)
		containerFound := false
		for i := range deployment.Spec.Template.Spec.Containers {
			if deployment.Spec.Template.Spec.Containers[i].Name == scalityUIComponent.Name {
				deployment.Spec.Template.Spec.Containers[i].Image = scalityUIComponent.Spec.Image
				containerFound = true
				break
			}
		}
		if !containerFound {
			deployment.Spec.Template.Spec.Containers = append(deployment.Spec.Template.Spec.Containers, corev1.Container{
				Name:  scalityUIComponent.Name,
				Image: scalityUIComponent.Spec.Image,
			})
		}

		// Update imagePullSecrets (empty slice clears them)
		deployment.Spec.Template.Spec.ImagePullSecrets = scalityUIComponent.Spec.ImagePullSecrets

		// Apply tolerations from CR spec (empty slice clears them)
		if scalityUIComponent.Spec.Scheduling != nil {
			deployment.Spec.Template.Spec.Tolerations = scalityUIComponent.Spec.Scheduling.Tolerations
		} else {
			deployment.Spec.Template.Spec.Tolerations = nil
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

		// Update selector
		if service.Spec.Selector == nil {
			service.Spec.Selector = make(map[string]string)
		}
		service.Spec.Selector["app"] = scalityUIComponent.Name

		// Update or add the http port (preserve other ports and existing port properties)
		desiredPort := corev1.ServicePort{
			Name:       "http",
			Port:       DefaultServicePort,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromInt(DefaultServicePort),
		}

		portFound := false
		for i := range service.Spec.Ports {
			if service.Spec.Ports[i].Name == "http" || service.Spec.Ports[i].Port == DefaultServicePort {
				// Preserve NodePort if it was assigned
				desiredPort.NodePort = service.Spec.Ports[i].NodePort
				service.Spec.Ports[i] = desiredPort
				portFound = true
				break
			}
		}
		if !portFound {
			service.Spec.Ports = append(service.Spec.Ports, desiredPort)
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
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Wait for rolling update to complete to avoid fetching from old pods
	if deployment.Status.UpdatedReplicas < deployment.Status.Replicas {
		logger.Info("Deployment still rolling out, will retry",
			"readyReplicas", deployment.Status.ReadyReplicas,
			"updatedReplicas", deployment.Status.UpdatedReplicas,
			"targetReplicas", deployment.Status.Replicas)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Extract current image from deployment by finding the matching container
	var currentImage string
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == scalityUIComponent.Name {
			currentImage = container.Image
			break
		}
	}
	if currentImage == "" {
		logger.Info("Cannot find UI component container in deployment")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Determine if we need to fetch configuration
	needsFetch := false
	reasons := []string{}

	// Check if this is the first fetch
	if scalityUIComponent.Status.LastFetchedImage == "" {
		needsFetch = true
		reasons = append(reasons, "initial configuration fetch")
	} else if scalityUIComponent.Status.LastFetchedImage != currentImage {
		// Image has changed, need to fetch new config
		needsFetch = true
		reasons = append(reasons, fmt.Sprintf("image changed from %s to %s",
			scalityUIComponent.Status.LastFetchedImage, currentImage))
	}

	// Check for force-refresh annotation
	if scalityUIComponent.Annotations != nil {
		if val, exists := scalityUIComponent.Annotations[uiv1alpha1.ForceRefreshAnnotation]; exists && val == "true" {
			needsFetch = true
			reasons = append(reasons, "force-refresh annotation present")
		}
	}

	// Skip fetch if not needed
	if !needsFetch {
		logger.V(1).Info("Skipping configuration fetch - image unchanged",
			"currentImage", currentImage,
			"lastFetchedImage", scalityUIComponent.Status.LastFetchedImage)
		return ctrl.Result{}, nil
	}

	logger.Info("Fetching micro-app configuration", "reasons", reasons, "image", currentImage)

	// Fetch configuration from service
	configContent, err := r.fetchMicroAppConfig(ctx, scalityUIComponent.Namespace, scalityUIComponent.Name)
	if err != nil {
		logger.Error(err, "Failed to fetch micro-app configuration")

		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeConfigurationRetrieved,
			Status:  metav1.ConditionFalse,
			Reason:  "FetchFailed",
			Message: fmt.Sprintf("Failed to fetch configuration from image %s: %v", currentImage, err),
		})

		// Update the status
		if statusErr := r.Status().Update(ctx, scalityUIComponent); statusErr != nil {
			logger.Error(statusErr, "Failed to update ScalityUIComponent status after fetch failure")
		}

		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Parse and apply configuration
	return r.parseAndApplyConfig(ctx, scalityUIComponent, configContent, currentImage)
}

func (r *ScalityUIComponentReconciler) parseAndApplyConfig(ctx context.Context,
	scalityUIComponent *uiv1alpha1.ScalityUIComponent, configContent string, currentImage string) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var config MicroAppConfig
	if err := json.Unmarshal([]byte(configContent), &config); err != nil {
		logger.Error(err, "Failed to parse micro-app configuration")

		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeConfigurationRetrieved,
			Status:  metav1.ConditionFalse,
			Reason:  "ParseFailed",
			Message: fmt.Sprintf("Failed to parse configuration from image %s: %v", currentImage, err),
		})

		// Update LastFetchedImage to prevent repeated fetch attempts for the same broken image
		scalityUIComponent.Status.LastFetchedImage = currentImage

		if statusErr := r.Status().Update(ctx, scalityUIComponent); statusErr != nil {
			logger.Error(statusErr, "Failed to update ScalityUIComponent status after parse failure")
		}

		// Remove force-refresh annotation even on failure to prevent infinite fetch loops
		r.removeForceRefreshAnnotation(ctx, scalityUIComponent)

		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Validate configuration
	if err := validateMicroAppConfig(&config); err != nil {
		logger.Error(err, "Invalid micro-app configuration")

		meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
			Type:    ConditionTypeConfigurationRetrieved,
			Status:  metav1.ConditionFalse,
			Reason:  "ValidationFailed",
			Message: fmt.Sprintf("Configuration validation failed for image %s: %v", currentImage, err),
		})

		// Update LastFetchedImage to prevent repeated fetch attempts for the same invalid config
		scalityUIComponent.Status.LastFetchedImage = currentImage

		if statusErr := r.Status().Update(ctx, scalityUIComponent); statusErr != nil {
			logger.Error(statusErr, "Failed to update ScalityUIComponent status after validation failure")
		}

		// Remove force-refresh annotation even on failure to prevent infinite fetch loops
		r.removeForceRefreshAnnotation(ctx, scalityUIComponent)

		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	existing := meta.FindStatusCondition(scalityUIComponent.Status.Conditions, ConditionTypeConfigurationRetrieved)
	conditionIsTrue := existing != nil &&
		existing.Status == metav1.ConditionTrue &&
		existing.Reason == ConditionReasonFetchSucceeded

	// Check if status actually changed to avoid unnecessary updates
	statusChanged := false
	if scalityUIComponent.Status.Kind != config.Metadata.Kind ||
		scalityUIComponent.Status.PublicPath != config.Spec.PublicPath ||
		scalityUIComponent.Status.Version != config.Spec.Version ||
		scalityUIComponent.Status.LastFetchedImage != currentImage {
		statusChanged = true
	}
	if !conditionIsTrue {
		statusChanged = true
	}

	if !statusChanged {
		logger.V(1).Info("Configuration unchanged, clearing force-refresh annotation")
		r.removeForceRefreshAnnotation(ctx, scalityUIComponent)
		return ctrl.Result{}, nil
	}

	// Update status with configuration details
	oldImage := scalityUIComponent.Status.LastFetchedImage

	scalityUIComponent.Status.Kind = config.Metadata.Kind
	scalityUIComponent.Status.PublicPath = config.Spec.PublicPath
	scalityUIComponent.Status.Version = config.Spec.Version
	scalityUIComponent.Status.LastFetchedImage = currentImage

	// Set appropriate condition message
	var conditionMessage string
	if oldImage == "" {
		conditionMessage = fmt.Sprintf("Successfully fetched initial configuration from image %s", currentImage)
	} else if oldImage != currentImage {
		conditionMessage = fmt.Sprintf("Configuration updated for new image: %s -> %s", oldImage, currentImage)
	} else {
		conditionMessage = fmt.Sprintf("Configuration verified for image %s", currentImage)
	}

	meta.SetStatusCondition(&scalityUIComponent.Status.Conditions, metav1.Condition{
		Type:    ConditionTypeConfigurationRetrieved,
		Status:  metav1.ConditionTrue,
		Reason:  ConditionReasonFetchSucceeded,
		Message: conditionMessage,
	})

	// Update the status first
	if err := r.Status().Update(ctx, scalityUIComponent); err != nil {
		logger.Error(err, "Failed to update ScalityUIComponent status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully updated ScalityUIComponent status",
		"kind", config.Metadata.Kind,
		"publicPath", config.Spec.PublicPath,
		"version", config.Spec.Version,
		"lastFetchedImage", currentImage)

	// Remove force-refresh annotation if it exists (also done on failure to prevent fetch loops)
	r.removeForceRefreshAnnotation(ctx, scalityUIComponent)

	return ctrl.Result{}, nil
}

func (r *ScalityUIComponentReconciler) removeForceRefreshAnnotation(ctx context.Context,
	scalityUIComponent *uiv1alpha1.ScalityUIComponent) {
	logger := ctrl.LoggerFrom(ctx)

	if scalityUIComponent.Annotations == nil {
		return
	}

	if _, exists := scalityUIComponent.Annotations[uiv1alpha1.ForceRefreshAnnotation]; !exists {
		return
	}

	key := client.ObjectKey{
		Name:      scalityUIComponent.Name,
		Namespace: scalityUIComponent.Namespace,
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &uiv1alpha1.ScalityUIComponent{}
		if err := r.Get(ctx, key, fresh); err != nil {
			return err
		}

		if fresh.Annotations == nil {
			return nil
		}

		if _, exists := fresh.Annotations[uiv1alpha1.ForceRefreshAnnotation]; !exists {
			return nil
		}

		delete(fresh.Annotations, uiv1alpha1.ForceRefreshAnnotation)
		return r.Update(ctx, fresh)
	})

	if err != nil {
		logger.Error(err, "Failed to remove force-refresh annotation after retries")
		return
	}

	logger.Info("Removed force-refresh annotation")
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
