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

type namespaceDeletionContextKey string

const namespaceDeletionNamespaceKey namespaceDeletionContextKey = "namespace-deletion-namespace"

func TestNamespaceDeletion_CascadeCleanup(t *testing.T) {
	const (
		scalityUIName = "cascade-cleanup-ui"
		componentName = "cascade-cleanup-component"
		exposerName   = "cascade-cleanup-exposer"
	)

	feature := features.New("namespace-deletion-cascade-cleanup").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			testNamespace := envconf.RandomName("ns-deletion", 16)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}
			t.Logf("Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, namespaceDeletionNamespaceKey, testNamespace)
			return ctx
		}).
		Assess("create ScalityUI", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Namespace Deletion Test").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUI: %v", err)
			}
			t.Logf("Created ScalityUI %s", scalityUIName)

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}
			t.Logf("ScalityUI is ready")

			return ctx
		}).
		Assess("create Component and Exposer in test namespace", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(namespaceDeletionNamespaceKey).(string)

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
			t.Logf("Component ready and configured")

			if err := framework.NewScalityUIComponentExposerBuilder(exposerName, namespace).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(componentName).
				WithAppHistoryBasePath("/cascade-cleanup").
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
		Assess("verify component in deployed-apps", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			if err := framework.WaitForDeployedAppsContains(ctx, client, scalityUIName, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component not in deployed-apps: %v", err)
			}
			t.Logf("Verified: Component %s is in deployed-apps ConfigMap", componentName)

			apps, err := framework.GetDeployedApps(ctx, client, scalityUIName)
			if err != nil {
				t.Fatalf("Failed to get deployed-apps: %v", err)
			}
			t.Logf("Deployed apps before deletion: %v", apps)

			return ctx
		}).
		Assess("delete namespace", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(namespaceDeletionNamespaceKey).(string)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: namespace},
			}
			if err := client.Resources().Delete(ctx, ns); err != nil {
				t.Fatalf("Failed to delete namespace: %v", err)
			}
			t.Logf("Triggered deletion of namespace %s", namespace)

			return ctx
		}).
		Assess("wait for namespace deletion", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(namespaceDeletionNamespaceKey).(string)

			if err := framework.WaitForNamespaceDeleted(ctx, client, namespace, framework.LongTimeout); err != nil {
				t.Fatalf("Namespace not deleted: %v", err)
			}
			t.Logf("Namespace %s fully deleted", namespace)

			return ctx
		}).
		Assess("verify component removed from deployed-apps", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			if err := framework.WaitForDeployedAppsNotContains(ctx, client, scalityUIName, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Component still in deployed-apps after namespace deletion: %v", err)
			}
			t.Logf("Verified: Component %s removed from deployed-apps ConfigMap", componentName)

			if err := framework.WaitForDeployedAppsCount(ctx, client, scalityUIName, 0, framework.DefaultTimeout); err != nil {
				t.Fatalf("Deployed-apps count not 0: %v", err)
			}
			t.Logf("Verified: deployed-apps is empty after namespace deletion")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := ctx.Value(namespaceDeletionNamespaceKey).(string)

			if err := framework.DeleteScalityUI(ctx, client, scalityUIName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI: %v", err)
			} else {
				t.Logf("Deleted ScalityUI %s", scalityUIName)
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
