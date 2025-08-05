package scalityui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newDeployedAppsConfigMapReducer creates a StateReducer for managing the deployed-ui-apps ConfigMap
func newDeployedAppsConfigMapReducer(r *ScalityUIReconciler, cr ScalityUI, currentState State) StateReducer {
	return StateReducer{
		F: func(cr ScalityUI, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			// Find all exposers that reference this UI using the service
			exposers, err := r.exposerService.FindAllExposersForUI(ctx, cr.Name)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to find exposers for UI %s: %w", cr.Name, err)
			}

			// Build the deployed apps list with component deduplication
			deployedApps := make([]DeployedUIApp, 0)
			processedComponents := make(map[string]bool) // Track which components we've already processed

			for _, exposer := range exposers {
				// Skip if we've already processed this component
				componentKey := exposer.Namespace + "/" + exposer.Spec.ScalityUIComponent
				if processedComponents[componentKey] {
					continue
				}

				component, err := r.exposerService.GetComponentForExposer(ctx, &exposer)
				if err != nil {
					log.Error(err, "Failed to get component for exposer, skipping this exposer",
						"exposer", exposer.Name,
						"exposerNamespace", exposer.Namespace,
						"componentName", exposer.Spec.ScalityUIComponent,
						"componentNamespace", exposer.Namespace,
						"uiName", cr.Name)
					continue // Skip this exposer but continue with others
				}

				if meta.IsStatusConditionTrue(component.Status.Conditions, "ConfigurationRetrieved") {
					deployedApp := DeployedUIApp{
						AppHistoryBasePath: exposer.Spec.AppHistoryBasePath,
						Kind:               component.Status.Kind,
						Name:               component.Name,
						URL:                component.Status.PublicPath,
						Version:            component.Status.Version,
					}
					deployedApps = append(deployedApps, deployedApp)
					processedComponents[componentKey] = true // Mark as processed
				}
			}

			// Create or update the deployed-ui-apps ConfigMap
			configMapName := cr.Name + "-deployed-ui-apps"
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: getOperatorNamespace(),
				},
			}

			deployedAppsJSON, err := json.MarshalIndent(deployedApps, "", "  ")
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to marshal deployed UI apps: %w", err)
			}

			h := sha256.New()
			h.Write(deployedAppsJSON)
			deployedAppsHash := fmt.Sprintf("%x", h.Sum(nil))

			result, err := controllerutil.CreateOrUpdate(ctx, currentState.GetKubeClient(), configMap, func() error {
				if err := controllerutil.SetControllerReference(cr, configMap, r.Scheme); err != nil {
					return fmt.Errorf("failed to set controller reference: %w", err)
				}

				if configMap.Data == nil {
					configMap.Data = make(map[string]string)
				}
				configMap.Data[deployedUIAppsKey] = string(deployedAppsJSON)

				if configMap.Annotations == nil {
					configMap.Annotations = make(map[string]string)
				}
				configMap.Annotations["scality.com/deployed-apps-hash"] = deployedAppsHash

				return nil
			})

			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to create or update deployed-ui-apps ConfigMap: %w", err)
			}

			logOperationResult(log, result, "ConfigMap deployed-ui-apps", configMapName)
			log.Info("Successfully reconciled deployed UI apps",
				"appsCount", len(deployedApps), "configMap", configMapName)

			return reconcile.Result{}, nil
		},
		N: "deployed-apps-configmap",
	}
}
