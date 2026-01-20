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

package e2e

import (
	"context"
	"testing"
	"time"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	"github.com/scality/ui-operator/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const (
	componentConfigHashAnnotation = "ui.scality.com/config-hash"
)

type podLifecycleContextKey string

const (
	podLifecycleNamespaceKey podLifecycleContextKey = "pod-lifecycle-namespace"
)

func TestPodLifecycle_RollingUpdateOnConfigChange(t *testing.T) {
	const (
		scalityUIName = "rolling-update-ui"
		componentName = "rolling-update-component"
		exposerName   = "rolling-update-exposer"
	)

	feature := features.New("rolling-update-on-config-change").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			testNamespace := envconf.RandomName("rolling-update", 16)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			t.Logf("Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, podLifecycleNamespaceKey, testNamespace)
			return ctx
		}).
		Assess("create full resource chain", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Rolling Update Test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUI: %v", err)
			}
			t.Logf("Created ScalityUI %s", scalityUIName)

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}

			if err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUIComponent: %v", err)
			}
			t.Logf("Created ScalityUIComponent %s", componentName)

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component not configured: %v", err)
			}

			if err := framework.NewScalityUIComponentExposerBuilder(exposerName, namespace).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(componentName).
				WithAppHistoryBasePath("/initial-path").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create Exposer: %v", err)
			}
			t.Logf("Created ScalityUIComponentExposer %s", exposerName)

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer not ready: %v", err)
			}

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component deployment not ready after exposer: %v", err)
			}

			return ctx
		}).
		Assess("wait for stability and record initial state", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			activeRS, err := framework.WaitForDeploymentStable(ctx, client, namespace, componentName, framework.LongTimeout)
			if err != nil {
				t.Fatalf("Deployment not stable: %v", err)
			}
			t.Logf("Deployment stable with single active ReplicaSet: %s (replicas=%d)", activeRS.Name, activeRS.Status.Replicas)

			initialHash, err := framework.GetDeploymentPodTemplateHash(ctx, client, namespace, componentName, componentConfigHashAnnotation)
			if err != nil {
				t.Fatalf("Failed to get initial hash: %v", err)
			}
			t.Logf("Initial component config hash: %s", initialHash)

			ctx = context.WithValue(ctx, podLifecycleContextKey("initial-hash"), initialHash)
			ctx = context.WithValue(ctx, podLifecycleContextKey("initial-rs-name"), activeRS.Name)
			return ctx
		}).
		Assess("modify exposer appHistoryBasePath", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				exposer := &uiv1alpha1.ScalityUIComponentExposer{}
				if err := client.Resources(namespace).Get(ctx, exposerName, namespace, exposer); err != nil {
					return err
				}
				exposer.Spec.AppHistoryBasePath = "/updated-path"
				return client.Resources(namespace).Update(ctx, exposer)
			})
			if err != nil {
				t.Fatalf("Failed to update exposer: %v", err)
			}
			t.Logf("Updated exposer appHistoryBasePath to /updated-path")

			return ctx
		}).
		Assess("verify component deployment rolling update", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)
			initialHash := ctx.Value(podLifecycleContextKey("initial-hash")).(string)
			initialRSName := ctx.Value(podLifecycleContextKey("initial-rs-name")).(string)

			newHash, err := framework.WaitForDeploymentAnnotationChange(ctx, client, namespace, componentName,
				componentConfigHashAnnotation, initialHash, framework.LongTimeout)
			if err != nil {
				t.Fatalf("Hash annotation did not change: %v", err)
			}
			t.Logf("Component config hash changed: %s -> %s", initialHash, newHash)

			newRSName, err := framework.WaitForNewReplicaSet(ctx, client, namespace, componentName, []string{initialRSName}, framework.LongTimeout)
			if err != nil {
				t.Fatalf("New ReplicaSet not created: %v", err)
			}
			t.Logf("New ReplicaSet created: %s", newRSName)

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not ready after rolling update: %v", err)
			}

			if err := framework.WaitForReplicaSetScaledDown(ctx, client, namespace, initialRSName, framework.LongTimeout); err != nil {
				t.Fatalf("Old ReplicaSet %s did not scale down: %v", initialRSName, err)
			}
			t.Logf("Old ReplicaSet %s scaled down to 0", initialRSName)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			if err := framework.DeleteScalityUI(ctx, client, scalityUIName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI: %v", err)
			}

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			if err := client.Resources().Delete(ctx, ns); err != nil {
				t.Logf("Warning: Failed to delete namespace: %v", err)
			} else {
				t.Logf("Deleted namespace %s", namespace)
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestPodLifecycle_OperatorCrashRecovery(t *testing.T) {
	const (
		scalityUIName = "crash-recovery-ui"
		componentName = "crash-recovery-component"
		exposerName   = "crash-recovery-exposer"
	)

	feature := features.New("operator-crash-recovery").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			testNamespace := envconf.RandomName("crash-recovery", 16)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			t.Logf("Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, podLifecycleNamespaceKey, testNamespace)
			return ctx
		}).
		Assess("create full resource chain", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Crash Recovery Test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUI: %v", err)
			}

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}
			t.Logf("ScalityUI ready")

			if err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create component: %v", err)
			}

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component not configured: %v", err)
			}
			t.Logf("Component ready and configured")

			if err := framework.NewScalityUIComponentExposerBuilder(exposerName, namespace).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(componentName).
				WithAppHistoryBasePath("/crash-test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create exposer: %v", err)
			}

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer not ready: %v", err)
			}
			t.Logf("Exposer ready - full chain established")

			return ctx
		}).
		Assess("delete operator pod (simulate crash)", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			t.Logf("Deleting operator pod to simulate crash...")
			if err := framework.DeleteOperatorPod(ctx, client, framework.LongTimeout); err != nil {
				t.Fatalf("Failed to restart operator: %v", err)
			}
			t.Logf("Operator pod restarted successfully")

			return ctx
		}).
		Assess("verify all resources recovered", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			time.Sleep(5 * time.Second)

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.DefaultTimeout); err != nil {
				t.Fatalf("ScalityUI not ready after recovery: %v", err)
			}
			t.Logf("ScalityUI still ready after crash recovery")

			if err := framework.WaitForScalityUIComponentCondition(ctx, client, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionTrue, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component ConfigurationRetrieved not True: %v", err)
			}
			t.Logf("Component ConfigurationRetrieved still True")

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Exposer not ready after recovery: %v", err)
			}
			t.Logf("Exposer still ready after crash recovery")

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component deployment not ready: %v", err)
			}
			t.Logf("Component deployment still running")

			if err := framework.WaitForServiceExists(ctx, client, namespace, componentName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component service not found: %v", err)
			}
			t.Logf("Component service still exists")

			runtimeConfigMapName := componentName + framework.RuntimeConfigMapSuffix
			if err := framework.WaitForConfigMapExists(ctx, client, namespace, runtimeConfigMapName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Runtime ConfigMap not found: %v", err)
			}
			t.Logf("Runtime ConfigMap still exists")

			t.Logf("All resources recovered successfully after operator crash")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			if err := framework.DeleteScalityUI(ctx, client, scalityUIName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI: %v", err)
			}

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			if err := client.Resources().Delete(ctx, ns); err != nil {
				t.Logf("Warning: Failed to delete namespace: %v", err)
			} else {
				t.Logf("Deleted namespace %s", namespace)
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestPodLifecycle_NoSpuriousUpdatesAfterRestart(t *testing.T) {
	const (
		scalityUIName = "no-spurious-ui"
		componentName = "no-spurious-component"
		exposerName   = "no-spurious-exposer"
	)

	feature := features.New("no-spurious-updates-after-restart").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			testNamespace := envconf.RandomName("no-spurious", 16)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			t.Logf("Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, podLifecycleNamespaceKey, testNamespace)
			return ctx
		}).
		Assess("create full resource chain and wait for stability", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("No Spurious Updates Test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUI: %v", err)
			}

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}

			if err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create component: %v", err)
			}

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component not configured: %v", err)
			}

			if err := framework.NewScalityUIComponentExposerBuilder(exposerName, namespace).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(componentName).
				WithAppHistoryBasePath("/no-spurious").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create exposer: %v", err)
			}

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer not ready: %v", err)
			}

			if _, err := framework.WaitForDeploymentStable(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not stable after exposer: %v", err)
			}

			t.Logf("Full chain established and stable")
			return ctx
		}).
		Assess("record resource versions", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			refs := []framework.ResourceRef{
				{Kind: "Deployment", Namespace: namespace, Name: componentName},
				{Kind: "Service", Namespace: namespace, Name: componentName},
				{Kind: "ConfigMap", Namespace: namespace, Name: componentName + framework.RuntimeConfigMapSuffix},
			}

			versions, err := framework.GetResourceVersions(ctx, client, refs)
			if err != nil {
				t.Fatalf("Failed to get resource versions: %v", err)
			}

			for key, version := range versions {
				t.Logf("Initial ResourceVersion: %s = %s", key, version)
			}

			ctx = context.WithValue(ctx, podLifecycleContextKey("resource-versions"), versions)
			ctx = context.WithValue(ctx, podLifecycleContextKey("resource-refs"), refs)
			return ctx
		}).
		Assess("restart operator", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			t.Logf("Restarting operator...")
			if err := framework.DeleteOperatorPod(ctx, client, framework.LongTimeout); err != nil {
				t.Fatalf("Failed to restart operator: %v", err)
			}
			t.Logf("Operator restarted")

			return ctx
		}).
		Assess("trigger reconcile and verify no spurious updates", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)
			initialVersions := ctx.Value(podLifecycleContextKey("resource-versions")).(map[string]string)
			refs := ctx.Value(podLifecycleContextKey("resource-refs")).([]framework.ResourceRef)

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				component := &uiv1alpha1.ScalityUIComponent{}
				if err := client.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
					return err
				}
				if component.Labels == nil {
					component.Labels = make(map[string]string)
				}
				component.Labels["trigger-reconcile"] = "true"
				return client.Resources(namespace).Update(ctx, component)
			})
			if err != nil {
				t.Fatalf("Failed to trigger reconcile: %v", err)
			}
			t.Logf("Triggered reconcile via label update")

			t.Logf("Waiting 5s for any potential updates...")
			time.Sleep(5 * time.Second)

			newVersions, err := framework.GetResourceVersions(ctx, client, refs)
			if err != nil {
				t.Fatalf("Failed to get new resource versions: %v", err)
			}

			allUnchanged := true
			for key, initialVersion := range initialVersions {
				newVersion := newVersions[key]
				if initialVersion != newVersion {
					t.Errorf("ResourceVersion changed for %s: %s -> %s", key, initialVersion, newVersion)
					allUnchanged = false
				} else {
					t.Logf("ResourceVersion unchanged: %s = %s", key, newVersion)
				}
			}

			if allUnchanged {
				t.Logf("SUCCESS: No spurious updates after operator restart")
			} else {
				t.Fatalf("FAILED: Some resources were updated spuriously")
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(podLifecycleNamespaceKey).(string)

			if err := framework.DeleteScalityUI(ctx, client, scalityUIName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI: %v", err)
			}

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			if err := client.Resources().Delete(ctx, ns); err != nil {
				t.Logf("Warning: Failed to delete namespace: %v", err)
			} else {
				t.Logf("Deleted namespace %s", namespace)
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
