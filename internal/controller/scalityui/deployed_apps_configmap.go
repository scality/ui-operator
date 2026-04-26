package scalityui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"github.com/scality/ui-operator/internal/controller/scalityuicomponent"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// Upper bound on how long the reducer holds off writing a ConfigMap
	// that would be missing components whose ConfigurationRetrieved
	// condition is not yet True. Past the cap we write what we have so a
	// single permanently-broken component cannot block the entire UI.
	partialWriteCap     = 60 * time.Second
	partialWriteRequeue = 5 * time.Second
)

// newDeployedAppsConfigMapReducer manages the deployed-ui-apps ConfigMap.
// In addition to writing the ConfigMap it tracks the first-seen-not-ready
// timestamp for each referenced component on the parent reconciler so the
// gate window is bounded across reconciles.
func newDeployedAppsConfigMapReducer(r *ScalityUIReconciler, cr ScalityUI, currentState State) StateReducer {
	return StateReducer{
		F: func(cr ScalityUI, currentState State, log logr.Logger) (reconcile.Result, error) {
			ctx := currentState.GetContext()

			exposers, err := r.exposerService.FindAllExposersForUI(ctx, cr.Name)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to find exposers for UI %s: %w", cr.Name, err)
			}

			deployedApps := make([]uiv1alpha1.DeployedUIApp, 0)
			processedComponents := make(map[string]bool)
			notReadyKeys := make([]string, 0)
			notReadyDisplay := make([]string, 0)

			for _, exposer := range exposers {
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
					continue
				}

				if !meta.IsStatusConditionTrue(component.Status.Conditions, scalityuicomponent.ConditionTypeConfigurationRetrieved) {
					notReadyKeys = append(notReadyKeys, notReadySinceKey(cr.Name, component.Namespace, component.Name))
					notReadyDisplay = append(notReadyDisplay, component.Namespace+"/"+component.Name)
					processedComponents[componentKey] = true
					continue
				}

				deployedApp := uiv1alpha1.DeployedUIApp{
					AppHistoryBasePath: exposer.Spec.AppHistoryBasePath,
					Kind:               component.Status.Kind,
					Name:               component.Name,
					URL:                component.Status.PublicPath,
					Version:            component.Status.Version,
				}
				deployedApps = append(deployedApps, deployedApp)
				processedComponents[componentKey] = true
			}

			deployedApps = append(deployedApps, cr.Spec.ExtraUIApps...)

			// Sort to keep the rendered JSON byte-stable. The exposer list
			// from the cache and the user-provided ExtraUIApps both have
			// non-canonical order; without this sort the SHA256 below
			// flaps and the Deployment template gets re-patched every
			// reconcile for no real change.
			//
			// SliceStable is used because (Name, URL) is not guaranteed
			// unique once user-provided ExtraUIApps mix in, and a stable
			// sort keeps the relative order of duplicates predictable for
			// debugging.
			sort.SliceStable(deployedApps, func(i, j int) bool {
				if deployedApps[i].Name != deployedApps[j].Name {
					return deployedApps[i].Name < deployedApps[j].Name
				}
				return deployedApps[i].URL < deployedApps[j].URL
			})

			// Drop entries from the per-component first-seen tracker that
			// are no longer not-ready, otherwise stale timestamps would
			// linger forever.
			r.gcNotReadySince(cr.Name, notReadyKeys)

			configMapName := cr.Name + "-deployed-ui-apps"
			cmKey := types.NamespacedName{Name: configMapName, Namespace: getOperatorNamespace()}

			// Hold off writing while any component is not yet ready, but
			// only when the ConfigMap already exists. There is no value in
			// gating a fresh install (no previous full state to protect),
			// and gating it would hang the rest of the reducer chain
			// (Deployment, Service, Ingress, status) for the entire wait
			// window.
			if len(notReadyKeys) > 0 {
				cmExists, err := r.configMapExists(ctx, currentState, cmKey)
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to check existing ConfigMap: %w", err)
				}
				if cmExists {
					if requeue, ok := r.shouldHoldOffPartialWrite(notReadyKeys); ok {
						log.Info("Holding off ConfigMap write while components reach readiness",
							"notReady", notReadyDisplay,
							"requeueAfter", requeue.String(),
							"cap", partialWriteCap.String())
						return reconcile.Result{RequeueAfter: requeue}, nil
					}
					log.Info("Cap exceeded, writing partial ConfigMap to avoid blocking UI indefinitely",
						"notReady", notReadyDisplay,
						"componentsIncluded", len(deployedApps)-len(cr.Spec.ExtraUIApps))
				} else {
					// Fresh install: still record first-seen so the gate
					// activates on subsequent reconciles, but do not hold
					// off the very first write.
					now := time.Now()
					for _, k := range notReadyKeys {
						r.notReadySince.LoadOrStore(k, now)
					}
				}
			}

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

			// Captured inside the mutation closure so the post-write log
			// can show old/new and surface unexpected rewrites in real time.
			oldHash := ""
			result, err := controllerutil.CreateOrUpdate(ctx, currentState.GetKubeClient(), configMap, func() error {
				if err := controllerutil.SetControllerReference(cr, configMap, r.Scheme); err != nil {
					return fmt.Errorf("failed to set controller reference: %w", err)
				}

				oldHash = configMap.Annotations[deployedAppsHashAnnotation]

				if configMap.Data == nil {
					configMap.Data = make(map[string]string)
				}
				configMap.Data[deployedUIAppsKey] = string(deployedAppsJSON)

				if configMap.Annotations == nil {
					configMap.Annotations = make(map[string]string)
				}
				configMap.Annotations[deployedAppsHashAnnotation] = deployedAppsHash

				return nil
			})

			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to create or update deployed-ui-apps ConfigMap: %w", err)
			}

			logOperationResult(log, result, "ConfigMap deployed-ui-apps", configMapName)
			if result == controllerutil.OperationResultUpdated && oldHash != "" && oldHash != deployedAppsHash {
				log.Info("deployed-apps-hash changed",
					"old", oldHash,
					"new", deployedAppsHash,
					"appCount", len(deployedApps))
			}
			log.Info("Successfully reconciled deployed UI apps",
				"totalAppsCount", len(deployedApps),
				"exposerAppsCount", len(deployedApps)-len(cr.Spec.ExtraUIApps),
				"extraUIAppsCount", len(cr.Spec.ExtraUIApps),
				"configMap", configMapName)

			// Store hash in memory for deployment to use (avoids cache sync issues)
			currentState.SetSubresourceHash(deployedAppsConfigMapHashKey, deployedAppsHash)

			return reconcile.Result{}, nil
		},
		N: "deployed-apps-configmap",
	}
}

// configMapExists reports whether the deployed-ui-apps ConfigMap is
// known to the controller-runtime cached client, treating IsNotFound
// as a non-error. The cache is eventually consistent, so a freshly
// created ConfigMap may briefly read as not-existing, which is
// acceptable: the next reconcile will see it.
func (r *ScalityUIReconciler) configMapExists(ctx context.Context, state State, key types.NamespacedName) (bool, error) {
	cm := &corev1.ConfigMap{}
	if err := state.GetKubeClient().Get(ctx, key, cm); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// shouldHoldOffPartialWrite records first-seen timestamps for the given
// not-ready components and reports whether at least one is still within
// the partial-write cap. The returned duration is the requeue delay to
// use when ok is true.
func (r *ScalityUIReconciler) shouldHoldOffPartialWrite(notReadyKeys []string) (time.Duration, bool) {
	now := time.Now()
	withinCap := false
	for _, k := range notReadyKeys {
		firstSeenIface, _ := r.notReadySince.LoadOrStore(k, now)
		firstSeen, ok := firstSeenIface.(time.Time)
		if !ok {
			// Defensive: someone stored an unexpected type. Reset and treat
			// this component as freshly observed.
			r.notReadySince.Store(k, now)
			withinCap = true
			continue
		}
		if now.Sub(firstSeen) < partialWriteCap {
			withinCap = true
		}
	}
	return partialWriteRequeue, withinCap
}

// gcNotReadySince removes tracked entries under uiName whose component is
// no longer in the currently-observed not-ready set. Called every
// reconcile so recovered components release their slot immediately.
func (r *ScalityUIReconciler) gcNotReadySince(uiName string, currentNotReadyKeys []string) {
	if len(currentNotReadyKeys) == 0 {
		r.clearNotReadySinceFor(uiName)
		return
	}
	keep := make(map[string]struct{}, len(currentNotReadyKeys))
	for _, k := range currentNotReadyKeys {
		keep[k] = struct{}{}
	}
	prefix := notReadySincePrefix(uiName)
	r.notReadySince.Range(func(k, _ any) bool {
		s, ok := k.(string)
		if !ok {
			return true
		}
		if !strings.HasPrefix(s, prefix) {
			return true
		}
		if _, still := keep[s]; !still {
			r.notReadySince.Delete(s)
		}
		return true
	})
}
