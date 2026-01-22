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

	"github.com/scality/ui-operator/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const (
	configMapFinalizerPrefix = "uicomponentexposer.scality.com/"
	configsSubdirectory      = "configs"
	defaultMountPath         = "/app/config"
)

type cascadeGCContextKey string

const (
	cascadeGCNamespaceKey cascadeGCContextKey = "cascade-gc-namespace"
	cascadeGCScalityUIKey cascadeGCContextKey = "cascade-gc-scalityui"
	cascadeGCComponentKey cascadeGCContextKey = "cascade-gc-component"
	cascadeGCExposerKey   cascadeGCContextKey = "cascade-gc-exposer"
)

func TestCascadeGC_ExposerUpdatesComponent(t *testing.T) {
	feature := features.New("exposer-updates-component").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			testNamespace := envconf.RandomName("cascade-update", 16)
			scalityUIName := envconf.RandomName("cascade-update-ui", 24)
			componentName := envconf.RandomName("cascade-update-comp", 24)
			exposerName := envconf.RandomName("cascade-update-exp", 24)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			t.Logf("Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, cascadeGCNamespaceKey, testNamespace)
			ctx = context.WithValue(ctx, cascadeGCScalityUIKey, scalityUIName)
			ctx = context.WithValue(ctx, cascadeGCComponentKey, componentName)
			ctx = context.WithValue(ctx, cascadeGCExposerKey, exposerName)
			return ctx
		}).
		Assess("create ScalityUI and Component without exposer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			scalityUIName := ctx.Value(cascadeGCScalityUIKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Cascade Update Test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUI: %v", err)
			}
			t.Logf("Created ScalityUI %s", scalityUIName)

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}

			if err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				WithMountPath(defaultMountPath).
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
			t.Logf("Component ready and configured (no exposer yet)")

			return ctx
		}).
		Assess("verify no config volume before exposer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)

			volumeName := framework.ConfigVolumePrefix + componentName
			err := framework.WaitForDeploymentNoVolume(ctx, client, namespace, componentName, volumeName, framework.DefaultTimeout)
			if err != nil {
				t.Fatalf("Expected no config volume before exposer: %v", err)
			}
			t.Logf("Verified: No config volume %s exists before exposer", volumeName)

			return ctx
		}).
		Assess("record initial ReplicaSet", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)

			activeRS, err := framework.WaitForDeploymentStable(ctx, client, namespace, componentName, framework.LongTimeout)
			if err != nil {
				t.Fatalf("Deployment not stable: %v", err)
			}
			t.Logf("Initial ReplicaSet: %s", activeRS.Name)

			ctx = context.WithValue(ctx, cascadeGCContextKey("initial-rs-name"), activeRS.Name)
			return ctx
		}).
		Assess("create exposer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			scalityUIName := ctx.Value(cascadeGCScalityUIKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)
			exposerName := ctx.Value(cascadeGCExposerKey).(string)

			if err := framework.NewScalityUIComponentExposerBuilder(exposerName, namespace).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(componentName).
				WithAppHistoryBasePath("/cascade-test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create Exposer: %v", err)
			}
			t.Logf("Created ScalityUIComponentExposer %s", exposerName)

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer not ready: %v", err)
			}
			t.Logf("Exposer is ready")

			return ctx
		}).
		Assess("verify volume added to deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)

			volumeName := framework.ConfigVolumePrefix + componentName
			if err := framework.WaitForDeploymentHasVolume(ctx, client, namespace, componentName, volumeName, framework.LongTimeout); err != nil {
				t.Fatalf("Volume not added to deployment: %v", err)
			}
			t.Logf("Verified: Volume %s added to deployment", volumeName)

			expectedMountPath := defaultMountPath + "/" + configsSubdirectory
			if err := framework.WaitForDeploymentHasVolumeMount(ctx, client, namespace, componentName, volumeName, expectedMountPath, framework.LongTimeout); err != nil {
				t.Fatalf("VolumeMount not added: %v", err)
			}
			t.Logf("Verified: VolumeMount %s -> %s added", volumeName, expectedMountPath)

			return ctx
		}).
		Assess("verify new ReplicaSet created (rolling update)", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)
			initialRSName := ctx.Value(cascadeGCContextKey("initial-rs-name")).(string)

			newRSName, err := framework.WaitForNewReplicaSet(ctx, client, namespace, componentName, []string{initialRSName}, framework.LongTimeout)
			if err != nil {
				t.Fatalf("New ReplicaSet not created: %v", err)
			}
			t.Logf("Verified: New ReplicaSet created: %s (initial was %s)", newRSName, initialRSName)

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not ready after rolling update: %v", err)
			}
			t.Logf("Deployment ready after rolling update")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			scalityUIName := ctx.Value(cascadeGCScalityUIKey).(string)

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

func TestCascadeGC_ExposerDeletionCleanup(t *testing.T) {
	feature := features.New("exposer-deletion-cleanup").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			testNamespace := envconf.RandomName("cascade-delete", 16)
			scalityUIName := envconf.RandomName("cascade-delete-ui", 24)
			componentName := envconf.RandomName("cascade-delete-comp", 24)
			exposerName := envconf.RandomName("cascade-delete-exp", 24)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			t.Logf("Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, cascadeGCNamespaceKey, testNamespace)
			ctx = context.WithValue(ctx, cascadeGCScalityUIKey, scalityUIName)
			ctx = context.WithValue(ctx, cascadeGCComponentKey, componentName)
			ctx = context.WithValue(ctx, cascadeGCExposerKey, exposerName)
			return ctx
		}).
		Assess("create full resource chain", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			scalityUIName := ctx.Value(cascadeGCScalityUIKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)
			exposerName := ctx.Value(cascadeGCExposerKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Cascade Delete Test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUI: %v", err)
			}

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}
			t.Logf("ScalityUI ready")

			if err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				WithMountPath(defaultMountPath).
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create component: %v", err)
			}

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component not configured: %v", err)
			}
			t.Logf("Component ready")

			if err := framework.NewScalityUIComponentExposerBuilder(exposerName, namespace).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(componentName).
				WithAppHistoryBasePath("/cascade-delete").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create exposer: %v", err)
			}

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer not ready: %v", err)
			}
			t.Logf("Full chain established")

			return ctx
		}).
		Assess("wait for stable state with volume mounted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)

			volumeName := framework.ConfigVolumePrefix + componentName
			if err := framework.WaitForDeploymentHasVolume(ctx, client, namespace, componentName, volumeName, framework.LongTimeout); err != nil {
				t.Fatalf("Volume not mounted: %v", err)
			}

			if _, err := framework.WaitForDeploymentStable(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not stable: %v", err)
			}
			t.Logf("Deployment stable with volume mounted")

			return ctx
		}).
		Assess("verify ConfigMap exists with finalizer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)
			exposerName := ctx.Value(cascadeGCExposerKey).(string)

			configMapName := componentName + framework.RuntimeConfigMapSuffix
			if err := framework.WaitForConfigMapExists(ctx, client, namespace, configMapName, framework.DefaultTimeout); err != nil {
				t.Fatalf("ConfigMap not found: %v", err)
			}
			t.Logf("ConfigMap %s exists", configMapName)

			finalizer := configMapFinalizerPrefix + exposerName
			if err := framework.WaitForConfigMapHasFinalizer(ctx, client, namespace, configMapName, finalizer, framework.DefaultTimeout); err != nil {
				t.Fatalf("ConfigMap missing finalizer: %v", err)
			}
			t.Logf("ConfigMap has finalizer %s", finalizer)

			return ctx
		}).
		Assess("delete exposer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			exposerName := ctx.Value(cascadeGCExposerKey).(string)

			if err := framework.DeleteScalityUIComponentExposer(ctx, client, namespace, exposerName); err != nil {
				t.Fatalf("Failed to delete exposer: %v", err)
			}
			t.Logf("Deleted exposer %s", exposerName)

			return ctx
		}).
		Assess("verify volume removed from deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)

			volumeName := framework.ConfigVolumePrefix + componentName
			if err := framework.WaitForDeploymentNoVolume(ctx, client, namespace, componentName, volumeName, framework.LongTimeout); err != nil {
				t.Fatalf("Volume not removed: %v", err)
			}
			t.Logf("Verified: Volume %s removed from deployment", volumeName)

			return ctx
		}).
		Assess("verify ConfigMap finalizer removed and ConfigMap deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			componentName := ctx.Value(cascadeGCComponentKey).(string)
			exposerName := ctx.Value(cascadeGCExposerKey).(string)

			configMapName := componentName + framework.RuntimeConfigMapSuffix
			finalizer := configMapFinalizerPrefix + exposerName

			if err := framework.WaitForConfigMapNoFinalizer(ctx, client, namespace, configMapName, finalizer, framework.LongTimeout); err != nil {
				t.Fatalf("Finalizer not removed: %v", err)
			}
			t.Logf("Verified: Finalizer %s removed", finalizer)

			if err := framework.WaitForConfigMapDeleted(ctx, client, namespace, configMapName, framework.LongTimeout); err != nil {
				t.Fatalf("ConfigMap not deleted by GC: %v", err)
			}
			t.Logf("Verified: ConfigMap %s deleted by GC", configMapName)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(cascadeGCNamespaceKey).(string)
			scalityUIName := ctx.Value(cascadeGCScalityUIKey).(string)

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
