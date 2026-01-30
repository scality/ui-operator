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

type multiNamespaceContextKey string

const (
	multiNamespaceNSAKey       multiNamespaceContextKey = "multi-ns-a"
	multiNamespaceNSBKey       multiNamespaceContextKey = "multi-ns-b"
	multiNamespaceScalityUIKey multiNamespaceContextKey = "multi-ns-scalityui"
	multiNamespaceCompA1Key    multiNamespaceContextKey = "multi-ns-comp-a1"
	multiNamespaceCompA2Key    multiNamespaceContextKey = "multi-ns-comp-a2"
	multiNamespaceCompB1Key    multiNamespaceContextKey = "multi-ns-comp-b1"
	multiNamespaceExpA1Key     multiNamespaceContextKey = "multi-ns-exp-a1"
	multiNamespaceExpA2Key     multiNamespaceContextKey = "multi-ns-exp-a2"
	multiNamespaceExpB1Key     multiNamespaceContextKey = "multi-ns-exp-b1"
)

func TestMultiNamespace_MultipleComponentsAggregation(t *testing.T) {
	t.Parallel()
	feature := features.New("multi-namespace-components-aggregation").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			nsA := envconf.RandomName("multi-ns-a", 16)
			nsB := envconf.RandomName("multi-ns-b", 16)
			scalityUIName := envconf.RandomName("multi-ns-agg-ui", 24)
			compA1Name := envconf.RandomName("comp-a1", 16)
			compA2Name := envconf.RandomName("comp-a2", 16)
			compB1Name := envconf.RandomName("comp-b1", 16)
			expA1Name := envconf.RandomName("exp-a1", 16)
			expA2Name := envconf.RandomName("exp-a2", 16)
			expB1Name := envconf.RandomName("exp-b1", 16)

			for _, nsName := range []string{nsA, nsB} {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: nsName},
				}
				if err := client.Resources().Create(ctx, ns); err != nil {
					t.Fatalf("Failed to create namespace %s: %v", nsName, err)
				}
				t.Logf("Created namespace %s", nsName)
			}

			ctx = context.WithValue(ctx, multiNamespaceNSAKey, nsA)
			ctx = context.WithValue(ctx, multiNamespaceNSBKey, nsB)
			ctx = context.WithValue(ctx, multiNamespaceScalityUIKey, scalityUIName)
			ctx = context.WithValue(ctx, multiNamespaceCompA1Key, compA1Name)
			ctx = context.WithValue(ctx, multiNamespaceCompA2Key, compA2Name)
			ctx = context.WithValue(ctx, multiNamespaceCompB1Key, compB1Name)
			ctx = context.WithValue(ctx, multiNamespaceExpA1Key, expA1Name)
			ctx = context.WithValue(ctx, multiNamespaceExpA2Key, expA2Name)
			ctx = context.WithValue(ctx, multiNamespaceExpB1Key, expB1Name)
			return ctx
		}).
		Assess("create ScalityUI", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Multi-Namespace Aggregation Test").
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
		Assess("create 2 Components in ns-a", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)
			compA1Name := ctx.Value(multiNamespaceCompA1Key).(string)
			compA2Name := ctx.Value(multiNamespaceCompA2Key).(string)

			for _, compName := range []string{compA1Name, compA2Name} {
				if err := framework.NewScalityUIComponentBuilder(compName, nsA).
					WithImage(framework.MockServerImage).
					Create(ctx, client); err != nil {
					t.Fatalf("Failed to create ScalityUIComponent %s: %v", compName, err)
				}
				t.Logf("Created ScalityUIComponent %s in %s", compName, nsA)

				if err := framework.WaitForDeploymentReady(ctx, client, nsA, compName, framework.LongTimeout); err != nil {
					t.Fatalf("Component %s deployment not ready: %v", compName, err)
				}

				if err := framework.WaitForScalityUIComponentConfigured(ctx, client, nsA, compName, framework.LongTimeout); err != nil {
					t.Fatalf("Component %s not configured: %v", compName, err)
				}
				t.Logf("Component %s ready and configured", compName)
			}

			return ctx
		}).
		Assess("create 1 Component in ns-b", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)

			if err := framework.NewScalityUIComponentBuilder(compB1Name, nsB).
				WithImage(framework.MockServerImage).
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUIComponent %s: %v", compB1Name, err)
			}
			t.Logf("Created ScalityUIComponent %s in %s", compB1Name, nsB)

			if err := framework.WaitForDeploymentReady(ctx, client, nsB, compB1Name, framework.LongTimeout); err != nil {
				t.Fatalf("Component %s deployment not ready: %v", compB1Name, err)
			}

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, nsB, compB1Name, framework.LongTimeout); err != nil {
				t.Fatalf("Component %s not configured: %v", compB1Name, err)
			}
			t.Logf("Component %s ready and configured", compB1Name)

			return ctx
		}).
		Assess("create 2 Exposers in ns-a", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)
			compA1Name := ctx.Value(multiNamespaceCompA1Key).(string)
			compA2Name := ctx.Value(multiNamespaceCompA2Key).(string)
			expA1Name := ctx.Value(multiNamespaceExpA1Key).(string)
			expA2Name := ctx.Value(multiNamespaceExpA2Key).(string)

			exposers := []struct {
				name      string
				component string
				basePath  string
			}{
				{expA1Name, compA1Name, "/app-a1"},
				{expA2Name, compA2Name, "/app-a2"},
			}

			for _, exp := range exposers {
				if err := framework.NewScalityUIComponentExposerBuilder(exp.name, nsA).
					WithScalityUI(scalityUIName).
					WithScalityUIComponent(exp.component).
					WithAppHistoryBasePath(exp.basePath).
					Create(ctx, client); err != nil {
					t.Fatalf("Failed to create Exposer %s: %v", exp.name, err)
				}
				t.Logf("Created ScalityUIComponentExposer %s", exp.name)

				if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, nsA, exp.name, framework.LongTimeout); err != nil {
					t.Fatalf("Exposer %s not ready: %v", exp.name, err)
				}
				t.Logf("Exposer %s is ready", exp.name)
			}

			return ctx
		}).
		Assess("create 1 Exposer in ns-b", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)
			expB1Name := ctx.Value(multiNamespaceExpB1Key).(string)

			if err := framework.NewScalityUIComponentExposerBuilder(expB1Name, nsB).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(compB1Name).
				WithAppHistoryBasePath("/app-b1").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create Exposer %s: %v", expB1Name, err)
			}
			t.Logf("Created ScalityUIComponentExposer %s", expB1Name)

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, nsB, expB1Name, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer %s not ready: %v", expB1Name, err)
			}
			t.Logf("Exposer %s is ready", expB1Name)

			return ctx
		}).
		Assess("verify deployed-apps contains all 3 components", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)
			compA1Name := ctx.Value(multiNamespaceCompA1Key).(string)
			compA2Name := ctx.Value(multiNamespaceCompA2Key).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)

			if err := framework.WaitForDeployedAppsCount(ctx, client, scalityUIName, 3, framework.LongTimeout); err != nil {
				t.Fatalf("Deployed-apps count mismatch: %v", err)
			}
			t.Logf("Verified: deployed-apps contains 3 components")

			for _, compName := range []string{compA1Name, compA2Name, compB1Name} {
				if err := framework.WaitForDeployedAppsContains(ctx, client, scalityUIName, compName, framework.DefaultTimeout); err != nil {
					t.Fatalf("Component %s not in deployed-apps: %v", compName, err)
				}
				t.Logf("Verified: Component %s is in deployed-apps", compName)
			}

			apps, err := framework.GetDeployedApps(ctx, client, scalityUIName)
			if err != nil {
				t.Fatalf("Failed to get deployed-apps: %v", err)
			}

			expectedBasePaths := map[string]string{
				compA1Name: "/app-a1",
				compA2Name: "/app-a2",
				compB1Name: "/app-b1",
			}
			for _, app := range apps {
				expectedPath, ok := expectedBasePaths[app.Name]
				if !ok {
					t.Errorf("Unexpected component in deployed-apps: %s", app.Name)
					continue
				}
				if app.AppHistoryBasePath != expectedPath {
					t.Errorf("Component %s has wrong AppHistoryBasePath: expected %s, got %s",
						app.Name, expectedPath, app.AppHistoryBasePath)
				} else {
					t.Logf("Verified: Component %s has correct AppHistoryBasePath=%s", app.Name, app.AppHistoryBasePath)
				}
			}

			return ctx
		}).
		Assess("verify runtime ConfigMaps exist in both namespaces", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			compA1Name := ctx.Value(multiNamespaceCompA1Key).(string)
			compA2Name := ctx.Value(multiNamespaceCompA2Key).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)

			nsAConfigMaps := []string{
				compA1Name + framework.RuntimeConfigMapSuffix,
				compA2Name + framework.RuntimeConfigMapSuffix,
			}
			for _, cmName := range nsAConfigMaps {
				if err := framework.WaitForConfigMapExists(ctx, client, nsA, cmName, framework.DefaultTimeout); err != nil {
					t.Fatalf("ConfigMap %s/%s not found: %v", nsA, cmName, err)
				}
				t.Logf("Verified: ConfigMap %s/%s exists", nsA, cmName)
			}

			nsBConfigMap := compB1Name + framework.RuntimeConfigMapSuffix
			if err := framework.WaitForConfigMapExists(ctx, client, nsB, nsBConfigMap, framework.DefaultTimeout); err != nil {
				t.Fatalf("ConfigMap %s/%s not found: %v", nsB, nsBConfigMap, err)
			}
			t.Logf("Verified: ConfigMap %s/%s exists", nsB, nsBConfigMap)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)

			if err := framework.DeleteScalityUI(ctx, client, scalityUIName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI: %v", err)
			} else {
				t.Logf("Deleted ScalityUI %s", scalityUIName)
			}

			for _, nsName := range []string{nsA, nsB} {
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
				if err := client.Resources().Delete(ctx, ns); err != nil {
					t.Logf("Warning: Failed to delete namespace %s: %v", nsName, err)
				} else {
					t.Logf("Deleted namespace %s", nsName)
				}
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMultiNamespace_PartialNamespaceDeletion(t *testing.T) {
	t.Parallel()
	feature := features.New("multi-namespace-partial-deletion").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			nsA := envconf.RandomName("partial-ns-a", 16)
			nsB := envconf.RandomName("partial-ns-b", 16)
			scalityUIName := envconf.RandomName("partial-del-ui", 24)
			compA1Name := envconf.RandomName("comp-a1", 16)
			compA2Name := envconf.RandomName("comp-a2", 16)
			compB1Name := envconf.RandomName("comp-b1", 16)
			expA1Name := envconf.RandomName("exp-a1", 16)
			expA2Name := envconf.RandomName("exp-a2", 16)
			expB1Name := envconf.RandomName("exp-b1", 16)

			for _, nsName := range []string{nsA, nsB} {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: nsName},
				}
				if err := client.Resources().Create(ctx, ns); err != nil {
					t.Fatalf("Failed to create namespace %s: %v", nsName, err)
				}
				t.Logf("Created namespace %s", nsName)
			}

			ctx = context.WithValue(ctx, multiNamespaceNSAKey, nsA)
			ctx = context.WithValue(ctx, multiNamespaceNSBKey, nsB)
			ctx = context.WithValue(ctx, multiNamespaceScalityUIKey, scalityUIName)
			ctx = context.WithValue(ctx, multiNamespaceCompA1Key, compA1Name)
			ctx = context.WithValue(ctx, multiNamespaceCompA2Key, compA2Name)
			ctx = context.WithValue(ctx, multiNamespaceCompB1Key, compB1Name)
			ctx = context.WithValue(ctx, multiNamespaceExpA1Key, expA1Name)
			ctx = context.WithValue(ctx, multiNamespaceExpA2Key, expA2Name)
			ctx = context.WithValue(ctx, multiNamespaceExpB1Key, expB1Name)
			return ctx
		}).
		Assess("create ScalityUI", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)

			if err := framework.NewScalityUIBuilder(scalityUIName).
				WithProductName("Multi-Namespace Partial Deletion Test").
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
		Assess("create Components and Exposers in both namespaces", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)
			compA1Name := ctx.Value(multiNamespaceCompA1Key).(string)
			compA2Name := ctx.Value(multiNamespaceCompA2Key).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)
			expA1Name := ctx.Value(multiNamespaceExpA1Key).(string)
			expA2Name := ctx.Value(multiNamespaceExpA2Key).(string)
			expB1Name := ctx.Value(multiNamespaceExpB1Key).(string)

			nsAComponents := []string{compA1Name, compA2Name}
			for _, compName := range nsAComponents {
				if err := framework.NewScalityUIComponentBuilder(compName, nsA).
					WithImage(framework.MockServerImage).
					Create(ctx, client); err != nil {
					t.Fatalf("Failed to create ScalityUIComponent %s: %v", compName, err)
				}
				t.Logf("Created ScalityUIComponent %s in %s", compName, nsA)

				if err := framework.WaitForDeploymentReady(ctx, client, nsA, compName, framework.LongTimeout); err != nil {
					t.Fatalf("Component %s deployment not ready: %v", compName, err)
				}

				if err := framework.WaitForScalityUIComponentConfigured(ctx, client, nsA, compName, framework.LongTimeout); err != nil {
					t.Fatalf("Component %s not configured: %v", compName, err)
				}
				t.Logf("Component %s ready and configured", compName)
			}

			if err := framework.NewScalityUIComponentBuilder(compB1Name, nsB).
				WithImage(framework.MockServerImage).
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create ScalityUIComponent %s: %v", compB1Name, err)
			}
			t.Logf("Created ScalityUIComponent %s in %s", compB1Name, nsB)

			if err := framework.WaitForDeploymentReady(ctx, client, nsB, compB1Name, framework.LongTimeout); err != nil {
				t.Fatalf("Component %s deployment not ready: %v", compB1Name, err)
			}

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, nsB, compB1Name, framework.LongTimeout); err != nil {
				t.Fatalf("Component %s not configured: %v", compB1Name, err)
			}
			t.Logf("Component %s ready and configured", compB1Name)

			nsAExposers := []struct {
				name      string
				component string
				basePath  string
			}{
				{expA1Name, compA1Name, "/app-a1"},
				{expA2Name, compA2Name, "/app-a2"},
			}

			for _, exp := range nsAExposers {
				if err := framework.NewScalityUIComponentExposerBuilder(exp.name, nsA).
					WithScalityUI(scalityUIName).
					WithScalityUIComponent(exp.component).
					WithAppHistoryBasePath(exp.basePath).
					Create(ctx, client); err != nil {
					t.Fatalf("Failed to create Exposer %s: %v", exp.name, err)
				}
				t.Logf("Created ScalityUIComponentExposer %s", exp.name)

				if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, nsA, exp.name, framework.LongTimeout); err != nil {
					t.Fatalf("Exposer %s not ready: %v", exp.name, err)
				}
				t.Logf("Exposer %s is ready", exp.name)
			}

			if err := framework.NewScalityUIComponentExposerBuilder(expB1Name, nsB).
				WithScalityUI(scalityUIName).
				WithScalityUIComponent(compB1Name).
				WithAppHistoryBasePath("/app-b1").
				Create(ctx, client); err != nil {
				t.Fatalf("Failed to create Exposer %s: %v", expB1Name, err)
			}
			t.Logf("Created ScalityUIComponentExposer %s", expB1Name)

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, nsB, expB1Name, framework.LongTimeout); err != nil {
				t.Fatalf("Exposer %s not ready: %v", expB1Name, err)
			}
			t.Logf("Exposer %s is ready", expB1Name)

			return ctx
		}).
		Assess("verify deployed-apps contains all 3 components", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)

			if err := framework.WaitForDeployedAppsCount(ctx, client, scalityUIName, 3, framework.LongTimeout); err != nil {
				t.Fatalf("Deployed-apps count mismatch: %v", err)
			}
			t.Logf("Verified: deployed-apps contains 3 components before deletion")

			apps, err := framework.GetDeployedApps(ctx, client, scalityUIName)
			if err != nil {
				t.Fatalf("Failed to get deployed-apps: %v", err)
			}
			t.Logf("Deployed apps before ns-a deletion: %+v", apps)

			return ctx
		}).
		Assess("delete namespace ns-a", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: nsA},
			}
			if err := client.Resources().Delete(ctx, ns); err != nil {
				t.Fatalf("Failed to delete namespace %s: %v", nsA, err)
			}
			t.Logf("Triggered deletion of namespace %s", nsA)

			return ctx
		}).
		Assess("wait for namespace ns-a deletion", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)

			if err := framework.WaitForNamespaceDeleted(ctx, client, nsA, framework.LongTimeout); err != nil {
				t.Fatalf("Namespace %s not deleted: %v", nsA, err)
			}
			t.Logf("Namespace %s fully deleted", nsA)

			return ctx
		}).
		Assess("verify deployed-apps updated correctly", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)
			compA1Name := ctx.Value(multiNamespaceCompA1Key).(string)
			compA2Name := ctx.Value(multiNamespaceCompA2Key).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)

			if err := framework.WaitForDeployedAppsCount(ctx, client, scalityUIName, 1, framework.LongTimeout); err != nil {
				t.Fatalf("Deployed-apps count should be 1 after ns-a deletion: %v", err)
			}
			t.Logf("Verified: deployed-apps count is 1 after ns-a deletion")

			for _, compName := range []string{compA1Name, compA2Name} {
				if err := framework.WaitForDeployedAppsNotContains(ctx, client, scalityUIName, compName, framework.DefaultTimeout); err != nil {
					t.Fatalf("Component %s still in deployed-apps after ns-a deletion: %v", compName, err)
				}
				t.Logf("Verified: Component %s removed from deployed-apps", compName)
			}

			if err := framework.WaitForDeployedAppsContains(ctx, client, scalityUIName, compB1Name, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component %s should still be in deployed-apps: %v", compB1Name, err)
			}
			t.Logf("Verified: Component %s still in deployed-apps", compB1Name)

			apps, err := framework.GetDeployedApps(ctx, client, scalityUIName)
			if err != nil {
				t.Fatalf("Failed to get deployed-apps: %v", err)
			}
			t.Logf("Deployed apps after ns-a deletion: %+v", apps)

			return ctx
		}).
		Assess("verify ns-b resources unaffected", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			compB1Name := ctx.Value(multiNamespaceCompB1Key).(string)
			expB1Name := ctx.Value(multiNamespaceExpB1Key).(string)

			if err := framework.WaitForDeploymentReady(ctx, client, nsB, compB1Name, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component %s deployment in ns-b should still be ready: %v", compB1Name, err)
			}
			t.Logf("Verified: Component %s deployment in ns-b is still running", compB1Name)

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, nsB, expB1Name, framework.DefaultTimeout); err != nil {
				t.Fatalf("Exposer %s in ns-b should still be ready: %v", expB1Name, err)
			}
			t.Logf("Verified: Exposer %s in ns-b is still ready", expB1Name)

			nsBConfigMap := compB1Name + framework.RuntimeConfigMapSuffix
			if err := framework.WaitForConfigMapExists(ctx, client, nsB, nsBConfigMap, framework.DefaultTimeout); err != nil {
				t.Fatalf("ConfigMap %s/%s should still exist: %v", nsB, nsBConfigMap, err)
			}
			t.Logf("Verified: ConfigMap %s/%s still exists after ns-a deletion", nsB, nsBConfigMap)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			nsA := ctx.Value(multiNamespaceNSAKey).(string)
			nsB := ctx.Value(multiNamespaceNSBKey).(string)
			scalityUIName := ctx.Value(multiNamespaceScalityUIKey).(string)

			if err := framework.DeleteScalityUI(ctx, client, scalityUIName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI: %v", err)
			} else {
				t.Logf("Deleted ScalityUI %s", scalityUIName)
			}

			for _, nsName := range []string{nsA, nsB} {
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
				if err := client.Resources().Delete(ctx, ns); err != nil {
					t.Logf("Warning: Failed to delete namespace %s: %v", nsName, err)
				} else {
					t.Logf("Deleted namespace %s", nsName)
				}
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
