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
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/scality/ui-operator/test/e2e/framework"
)

func TestCRDInstallation(t *testing.T) {
	expectedCRDs := []string{
		"scalityuis.ui.scality.com",
		"scalityuicomponents.ui.scality.com",
		"scalityuicomponentexposers.ui.scality.com",
	}

	feature := features.New("crd-installation").
		Assess("all CRDs are registered and established", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			for _, crdName := range expectedCRDs {
				if err := framework.WaitForCRDEstablished(ctx, client, crdName); err != nil {
					t.Fatalf("CRD %s not established within timeout: %v", crdName, err)
				}
				t.Logf("✓ CRD %s is registered and established", crdName)
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestOperatorDeployment(t *testing.T) {
	if framework.SkipOperatorDeploy() {
		t.Skip("Skipping operator deployment test (E2E_SKIP_OPERATOR=true)")
	}

	feature := features.New("operator-deployment").
		Assess("controller-manager deployment is ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			err := framework.WaitForDeploymentReady(
				ctx,
				client,
				framework.OperatorNamespace,
				framework.OperatorDeployment,
				framework.LongTimeout,
			)
			if err != nil {
				t.Fatalf("Operator deployment not ready: %v", err)
			}
			t.Logf("✓ Deployment %s/%s is ready", framework.OperatorNamespace, framework.OperatorDeployment)

			return ctx
		}).
		Assess("controller-manager pod is running", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			pod, err := framework.WaitForPodRunning(
				ctx,
				client,
				framework.OperatorNamespace,
				framework.ControlPlaneLabel,
				framework.ControlPlaneValue,
				framework.LongTimeout,
			)
			if err != nil {
				t.Fatalf("Operator pod not running: %v", err)
			}
			t.Logf("✓ Pod %s is running", pod.Name)

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

type smokeTestContextKey string

const (
	smokeNamespaceKey  smokeTestContextKey = "smoke-namespace"
	smokeScalityUIName smokeTestContextKey = "smoke-scalityui-name"
)

func getContextString(ctx context.Context, t *testing.T, key smokeTestContextKey) string {
	val, ok := ctx.Value(key).(string)
	if !ok {
		t.Fatalf("Context key %q not found or not a string", key)
	}
	return val
}

func TestSmokeFullChain(t *testing.T) {
	const (
		scalityUIName = "e2e-smoke-ui"
		componentName = "e2e-smoke-component"
		exposerName   = "e2e-smoke-exposer"
	)

	// Expected values from mock-server's defaultMicroAppConfig()
	const (
		expectedPublicPath = "/mock/"
		expectedKind       = "shell"
		expectedVersion    = "1.0.0"
	)

	feature := features.New("smoke-full-chain").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			testNamespace := envconf.RandomName("e2e-smoke", 16)

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			if err := client.Resources().Create(ctx, ns); err != nil {
				t.Fatalf("Failed to create namespace %s: %v", testNamespace, err)
			}
			t.Logf("✓ Created namespace %s", testNamespace)

			ctx = context.WithValue(ctx, smokeNamespaceKey, testNamespace)
			ctx = context.WithValue(ctx, smokeScalityUIName, scalityUIName)
			return ctx
		}).
		Assess("create ScalityUI from YAML", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := getContextString(ctx, t, smokeNamespaceKey)

			yamlPath := filepath.Join(framework.GetTestDataPath("smoke"), "scalityui.yaml")
			if err := framework.LoadAndApplyYAML(ctx, client, yamlPath, namespace); err != nil {
				t.Fatalf("Failed to apply ScalityUI YAML: %v", err)
			}
			t.Logf("✓ Applied ScalityUI from YAML")

			if err := framework.WaitForScalityUIReady(ctx, client, scalityUIName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUI not ready: %v", err)
			}
			t.Logf("✓ ScalityUI is ready")

			return ctx
		}).
		Assess("create ScalityUIComponent from YAML", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := getContextString(ctx, t, smokeNamespaceKey)

			yamlPath := filepath.Join(framework.GetTestDataPath("smoke"), "scalityuicomponent.yaml")
			if err := framework.LoadAndApplyYAML(ctx, client, yamlPath, namespace); err != nil {
				t.Fatalf("Failed to apply ScalityUIComponent YAML: %v", err)
			}
			t.Logf("✓ Applied ScalityUIComponent from YAML")

			if err := framework.WaitForDeploymentReady(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUIComponent deployment not ready: %v", err)
			}
			t.Logf("✓ ScalityUIComponent deployment is ready")

			if err := framework.WaitForScalityUIComponentConfigured(ctx, client, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUIComponent not configured: %v", err)
			}
			t.Logf("✓ ScalityUIComponent configuration retrieved")

			return ctx
		}).
		Assess("create ScalityUIComponentExposer from YAML", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := getContextString(ctx, t, smokeNamespaceKey)

			yamlPath := filepath.Join(framework.GetTestDataPath("smoke"), "scalityuicomponentexposer.yaml")
			if err := framework.LoadAndApplyYAML(ctx, client, yamlPath, namespace); err != nil {
				t.Fatalf("Failed to apply ScalityUIComponentExposer YAML: %v", err)
			}
			t.Logf("✓ Applied ScalityUIComponentExposer from YAML")

			if err := framework.WaitForScalityUIComponentExposerReady(ctx, client, namespace, exposerName, framework.LongTimeout); err != nil {
				t.Fatalf("ScalityUIComponentExposer not ready: %v", err)
			}
			t.Logf("✓ ScalityUIComponentExposer is ready")

			return ctx
		}).
		Assess("verify all resources created correctly", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := getContextString(ctx, t, smokeNamespaceKey)

			if err := framework.WaitForServiceExists(ctx, client, namespace, componentName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component Service not found: %v", err)
			}
			t.Logf("✓ Component Service exists")

			runtimeConfigMapName := componentName + framework.RuntimeConfigMapSuffix
			if err := framework.WaitForConfigMapExists(ctx, client, namespace, runtimeConfigMapName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Runtime ConfigMap not found: %v", err)
			}
			t.Logf("✓ Runtime ConfigMap exists")

			component, err := framework.GetScalityUIComponent(ctx, client, namespace, componentName)
			if err != nil {
				t.Fatalf("Failed to get ScalityUIComponent: %v", err)
			}
			if component.Status.PublicPath != expectedPublicPath {
				t.Fatalf("Expected PublicPath %q, got %q", expectedPublicPath, component.Status.PublicPath)
			}
			t.Logf("✓ ScalityUIComponent.Status.PublicPath = %s", component.Status.PublicPath)

			if component.Status.Kind != expectedKind {
				t.Fatalf("Expected Kind %q, got %q", expectedKind, component.Status.Kind)
			}
			t.Logf("✓ ScalityUIComponent.Status.Kind = %s", component.Status.Kind)

			if component.Status.Version != expectedVersion {
				t.Fatalf("Expected Version %q, got %q", expectedVersion, component.Status.Version)
			}
			t.Logf("✓ ScalityUIComponent.Status.Version = %s", component.Status.Version)

			configVolumeName := framework.ConfigVolumePrefix + componentName
			if err := framework.WaitForDeploymentHasVolume(ctx, client, namespace, componentName, configVolumeName, framework.DefaultTimeout); err != nil {
				t.Fatalf("Component Deployment does not have config volume: %v", err)
			}
			t.Logf("✓ Component Deployment has config volume mounted")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()
			namespace := getContextString(ctx, t, smokeNamespaceKey)
			uiName := getContextString(ctx, t, smokeScalityUIName)

			if err := framework.DeleteScalityUI(ctx, client, uiName); err != nil {
				t.Logf("Warning: Failed to delete ScalityUI %s: %v", uiName, err)
			} else {
				t.Logf("✓ Deleted ScalityUI %s", uiName)
			}

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			if err := client.Resources().Delete(ctx, ns); err != nil {
				t.Logf("Warning: Failed to delete namespace %s: %v", namespace, err)
			} else {
				t.Logf("✓ Deleted namespace %s", namespace)
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
