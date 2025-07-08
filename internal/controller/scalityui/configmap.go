package scalityui

import (
	"crypto/sha256"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// newScalityUIConfigMapReducer creates a StateReducer for managing the main ScalityUI ConfigMap
func newScalityUIConfigMapReducer(r *ScalityUIReconciler, cr ScalityUI, currentState State) StateReducer {
	return StateReducer{
		F: func(cr ScalityUI, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			// Generate config JSON
			configJSON, err := createConfigJSON(cr)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to create configJSON: %w", err)
			}

			// Calculate hash of configJSON
			h := sha256.New()
			h.Write(configJSON)
			configHash := fmt.Sprintf("%x", h.Sum(nil))

			// Define ConfigMap for config.json
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Name,
					Namespace: getOperatorNamespace(),
				},
			}

			// Create or update ConfigMap
			result, err := controllerutil.CreateOrUpdate(ctx, currentState.GetKubeClient(), configMap, func() error {
				if err := controllerutil.SetControllerReference(cr, configMap, r.Scheme); err != nil {
					return fmt.Errorf("failed to set controller reference: %w", err)
				}

				if configMap.Data == nil {
					configMap.Data = make(map[string]string)
				}
				configMap.Data["config.json"] = string(configJSON)

				// Store the hash in annotations for deployment to use
				if configMap.Annotations == nil {
					configMap.Annotations = make(map[string]string)
				}
				configMap.Annotations["scality.com/config-hash"] = configHash

				return nil
			})

			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to create or update ConfigMap: %w", err)
			}

			logOperationResult(log, result, "ConfigMap", configMap.Name)
			return reconcile.Result{}, nil
		},
		N: "configmap",
	}
}
