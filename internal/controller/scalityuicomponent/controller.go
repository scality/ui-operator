package scalityuicomponent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/scality/reconciler-framework/reconciler"
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
	reconciler.BaseReconciler[ScalityUIComponent, State]
	ConfigFetcher ConfigFetcher
}

func NewScalityUIComponentReconciler(client client.Client, scheme *runtime.Scheme) *ScalityUIComponentReconciler {
	return &ScalityUIComponentReconciler{
		BaseReconciler: reconciler.BaseReconciler[ScalityUIComponent, State]{
			Client:       client,
			Scheme:       scheme,
			OperatorName: "ui-operator",
		},
	}
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents/finalizers,verbs=update

func (r *ScalityUIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	currentState := newReconcileContextWithCtx(ctx)
	currentState.SetLog(log)
	currentState.SetKubeClient(r.Client)

	cr := &uiv1alpha1.ScalityUIComponent{}
	err := r.Client.Get(ctx, req.NamespacedName, cr)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	resourceReconcilers := buildReducerList(r, cr, currentState)
	for _, rr := range resourceReconcilers {
		res, err := rr.F(cr, currentState, log)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to reconcile %s: %w", rr.N, err)
		}

		if res.Requeue || res.RequeueAfter > 0 {
			return res, nil
		}
	}

	log.Info("reconciliation successful")
	return reconcile.Result{}, nil
}

func buildReducerList(r *ScalityUIComponentReconciler, cr ScalityUIComponent, currentState State) []StateReducer {
	return []StateReducer{
		newServiceReducer(r),
		newDeploymentReducer(r),
		newConfigReducer(r),
	}
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
