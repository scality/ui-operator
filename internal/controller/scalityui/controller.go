package scalityui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/scality/reconciler-framework/reconciler"
	uiscalitycomv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/scality/ui-operator/internal/mappers"
	"github.com/scality/ui-operator/internal/services"
)

// DeployedUIApp represents a deployed UI application entry
type DeployedUIApp struct {
	AppHistoryBasePath string `json:"appHistoryBasePath"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	Version            string `json:"version"`
}

// getOperatorNamespace returns the namespace where the operator is deployed.
// It reads from the POD_NAMESPACE environment variable, which should be set
// using the downward API in the operator's deployment.
// Falls back to "scality-ui" if the environment variable is not set.
func getOperatorNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	// Fallback to default namespace if POD_NAMESPACE is not set
	return "scality-ui"
}

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

type ScalityUIReconciler struct {
	reconciler.BaseReconciler[ScalityUI, State]
	exposerService *services.ExposerService
	eventMapper    *mappers.UIEventMapper
}

func NewScalityUIReconciler(client client.Client, scheme *runtime.Scheme) *ScalityUIReconciler {
	return &ScalityUIReconciler{
		BaseReconciler: reconciler.BaseReconciler[ScalityUI, State]{
			Client:       client,
			Scheme:       scheme,
			OperatorName: "ui-operator",
		},
		exposerService: services.NewExposerService(client),
		eventMapper:    mappers.NewUIEventMapper(client),
	}
}

// NewScalityUIReconcilerForTest creates a ScalityUIReconciler configured for testing environments
func NewScalityUIReconcilerForTest(client client.Client, scheme *runtime.Scheme) *ScalityUIReconciler {
	return &ScalityUIReconciler{
		BaseReconciler: reconciler.BaseReconciler[ScalityUI, State]{
			Client:                   client,
			Scheme:                   scheme,
			OperatorName:             "ui-operator",
			SkipResourceSettledCheck: true, // Skip resource settled check for tests
		},
		exposerService: services.NewExposerService(client),
		eventMapper:    mappers.NewUIEventMapper(client),
	}
}

// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuis/finalizers,verbs=update
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponents,verbs=get;list;watch
// +kubebuilder:rbac:groups=ui.scality.com,resources=scalityuicomponentexposers,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

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
	log := log.FromContext(ctx)

	currentState := newReconcileContextWithCtx(ctx)
	currentState.SetLog(log)
	currentState.SetKubeClient(r.Client)

	cr := &uiscalitycomv1alpha1.ScalityUI{}
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

func buildReducerList(r *ScalityUIReconciler, cr ScalityUI, currentState State) []StateReducer {
	return []StateReducer{
		newScalityUIConfigMapReducer(r, cr, currentState),
		newDeployedAppsConfigMapReducer(r, cr, currentState),
		newDeploymentReducer(r),
		newServiceReducer(r),
		newIngressReducer(r),
		newStatusReducer(cr, currentState),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScalityUIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&uiscalitycomv1alpha1.ScalityUI{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Watches(&uiscalitycomv1alpha1.ScalityUIComponentExposer{}, handler.EnqueueRequestsFromMapFunc(r.eventMapper.MapExposerToUI)).
		Watches(&uiscalitycomv1alpha1.ScalityUIComponent{}, handler.EnqueueRequestsFromMapFunc(r.eventMapper.MapComponentToUI)).
		Complete(r)
}

// logOperationResult logs the result of a Kubernetes operation
func logOperationResult(logger logr.Logger, result controllerutil.OperationResult, resourceType string, resourceName string) {
	switch result {
	case controllerutil.OperationResultCreated:
		logger.Info(fmt.Sprintf("%s created", resourceType), "name", resourceName)
	case controllerutil.OperationResultUpdated:
		logger.Info(fmt.Sprintf("%s updated", resourceType), "name", resourceName)
	case controllerutil.OperationResultNone:
		// No change needed
	default:
		logger.Info(fmt.Sprintf("%s operation completed", resourceType), "name", resourceName, "result", result)
	}
}
