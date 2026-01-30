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
	"fmt"
	"strconv"
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

type realHTTPContextKey string

const realHTTPNamespaceKey realHTTPContextKey = "real-http-namespace"

func TestRealHTTP_ReconcileStormPrevention(t *testing.T) {
	const componentName = "storm-test-component"

	feature := features.New("reconcile-storm-prevention").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return setupNamespace(ctx, t, cfg)
		}).
		Assess("create component and wait for initial config fetch", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, k8sClient)
			if err != nil {
				t.Fatalf("Failed to create ScalityUIComponent: %v", err)
			}
			t.Logf("Created ScalityUIComponent %s", componentName)

			if err := framework.WaitForDeploymentReady(ctx, k8sClient, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionTrue, framework.LongTimeout); err != nil {
				t.Fatalf("Configuration not retrieved: %v", err)
			}
			t.Logf("Initial configuration retrieved")

			return ctx
		}).
		Assess("verify counter is 1 after initial fetch", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			counter, err := mockClient.GetCounter(ctx)
			if err != nil {
				t.Fatalf("Failed to get counter: %v", err)
			}
			if counter != 1 {
				t.Fatalf("Expected counter=1 after initial fetch, got %d", counter)
			}
			t.Logf("Counter is 1 after initial fetch")

			return ctx
		}).
		Assess("trigger 10 reconciles without changing image", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			for i := 0; i < 10; i++ {
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					component := &uiv1alpha1.ScalityUIComponent{}
					if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
						return err
					}
					if component.Labels == nil {
						component.Labels = make(map[string]string)
					}
					component.Labels["reconcile-trigger"] = strconv.Itoa(i)
					return k8sClient.Resources(namespace).Update(ctx, component)
				})
				if err != nil {
					t.Fatalf("Failed to update component on iteration %d: %v", i, err)
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Logf("Triggered 10 reconciles")

			time.Sleep(2 * time.Second)

			return ctx
		}).
		Assess("verify counter still equals 1", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			counter, err := mockClient.GetCounter(ctx)
			if err != nil {
				t.Fatalf("Failed to get counter: %v", err)
			}
			if counter != 1 {
				t.Fatalf("Expected counter=1 after 10 reconciles (no image change), got %d", counter)
			}
			t.Logf("Counter remains 1 - reconcile storm prevention works")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return teardownNamespace(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestRealHTTP_TimeoutHandling(t *testing.T) {
	const componentName = "timeout-test-component"

	feature := features.New("http-timeout-handling").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return setupNamespace(ctx, t, cfg)
		}).
		Assess("create component and wait for initial success", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, k8sClient)
			if err != nil {
				t.Fatalf("Failed to create ScalityUIComponent: %v", err)
			}
			t.Logf("Created ScalityUIComponent %s", componentName)

			if err := framework.WaitForDeploymentReady(ctx, k8sClient, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionTrue, framework.LongTimeout); err != nil {
				t.Fatalf("Initial configuration not retrieved: %v", err)
			}
			t.Logf("Initial configuration retrieved successfully")

			return ctx
		}).
		Assess("configure 15s delay and trigger re-fetch", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			if err := mockClient.SetDelay(ctx, 15000); err != nil {
				t.Fatalf("Failed to set delay: %v", err)
			}
			t.Logf("Configured mock server with 15s delay (operator timeout is 10s)")

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				component := &uiv1alpha1.ScalityUIComponent{}
				if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
					return err
				}
				if component.Annotations == nil {
					component.Annotations = make(map[string]string)
				}
				component.Annotations[uiv1alpha1.ForceRefreshAnnotation] = "true"
				return k8sClient.Resources(namespace).Update(ctx, component)
			})
			if err != nil {
				t.Fatalf("Failed to set force-refresh annotation: %v", err)
			}
			t.Logf("Set force-refresh annotation to trigger re-fetch")

			return ctx
		}).
		Assess("verify timeout failure", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			timeout := 30 * time.Second
			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionFalse, timeout); err != nil {
				t.Fatalf("Expected ConfigurationRetrieved=False due to timeout: %v", err)
			}

			component, err := framework.GetScalityUIComponent(ctx, k8sClient, namespace, componentName)
			if err != nil {
				t.Fatalf("Failed to get component: %v", err)
			}

			for _, cond := range component.Status.Conditions {
				if cond.Type == framework.ConditionConfigurationRetrieved {
					if cond.Reason != "FetchFailed" {
						t.Fatalf("Expected Reason=FetchFailed, got %s", cond.Reason)
					}
					t.Logf("Verified: ConfigurationRetrieved=False, Reason=FetchFailed")
					break
				}
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return teardownNamespace(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// TestRealHTTP_RecoveryAfterServerFailure verifies recovery after server failure.
// Strategy: Reset mock server config, then wait for controller's natural retry (RequeueAfter: 30s).
func TestRealHTTP_RecoveryAfterServerFailure(t *testing.T) {
	const componentName = "recovery-test-component"

	feature := features.New("recovery-after-server-failure").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return setupNamespace(ctx, t, cfg)
		}).
		Assess("create component and wait for initial success", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, k8sClient)
			if err != nil {
				t.Fatalf("Failed to create ScalityUIComponent: %v", err)
			}
			t.Logf("Created ScalityUIComponent %s", componentName)

			if err := framework.WaitForDeploymentReady(ctx, k8sClient, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionTrue, framework.LongTimeout); err != nil {
				t.Fatalf("Initial configuration not retrieved: %v", err)
			}
			t.Logf("Initial configuration retrieved successfully")

			return ctx
		}).
		Assess("configure 500 error and trigger re-fetch", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			if err := mockClient.SetStatusCode(ctx, 500); err != nil {
				t.Fatalf("Failed to set status code: %v", err)
			}
			t.Logf("Configured mock server to return 500")

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				component := &uiv1alpha1.ScalityUIComponent{}
				if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
					return err
				}
				if component.Annotations == nil {
					component.Annotations = make(map[string]string)
				}
				component.Annotations[uiv1alpha1.ForceRefreshAnnotation] = "true"
				return k8sClient.Resources(namespace).Update(ctx, component)
			})
			if err != nil {
				t.Fatalf("Failed to set force-refresh annotation: %v", err)
			}
			t.Logf("Set force-refresh annotation to trigger re-fetch")

			return ctx
		}).
		Assess("verify failure status", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionFalse, framework.LongTimeout); err != nil {
				t.Fatalf("Expected ConfigurationRetrieved=False: %v", err)
			}
			t.Logf("Verified: ConfigurationRetrieved=False (server returning 500)")

			return ctx
		}).
		Assess("recover mock server and trigger explicit re-fetch", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			// First, remove force-refresh so we can control when the next fetch happens
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				component := &uiv1alpha1.ScalityUIComponent{}
				if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
					return err
				}
				delete(component.Annotations, uiv1alpha1.ForceRefreshAnnotation)
				return k8sClient.Resources(namespace).Update(ctx, component)
			})
			if err != nil {
				t.Fatalf("Failed to remove force-refresh: %v", err)
			}
			t.Logf("Removed force-refresh annotation")

			if err := framework.WaitForScalityUIComponentAnnotationAbsent(ctx, k8sClient, namespace, componentName,
				uiv1alpha1.ForceRefreshAnnotation, framework.DefaultTimeout); err != nil {
				t.Fatalf("Failed to verify force-refresh annotation removed: %v", err)
			}

			// Reset mock server to return 200
			if err := mockClient.Reset(ctx); err != nil {
				t.Fatalf("Failed to reset mock server: %v", err)
			}
			t.Logf("Reset mock server to return 200")

			// Verify mock server is returning 200
			statusCode, _, err := mockClient.TestFetch(ctx)
			if err != nil {
				t.Fatalf("Failed to test fetch: %v", err)
			}
			if statusCode != 200 {
				t.Fatalf("Mock server should return 200, got %d", statusCode)
			}
			t.Logf("Verified: Mock server returns 200")

			// Now add force-refresh to trigger a new fetch
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				component := &uiv1alpha1.ScalityUIComponent{}
				if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
					return err
				}
				if component.Annotations == nil {
					component.Annotations = make(map[string]string)
				}
				component.Annotations[uiv1alpha1.ForceRefreshAnnotation] = "true"
				return k8sClient.Resources(namespace).Update(ctx, component)
			})
			if err != nil {
				t.Fatalf("Failed to add force-refresh: %v", err)
			}
			t.Logf("Added force-refresh to trigger new fetch")

			// Wait for condition to become True
			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionTrue, 30*time.Second); err != nil {
				// Debug info
				component := &uiv1alpha1.ScalityUIComponent{}
				_ = k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component)
				t.Logf("Debug: Annotations=%v", component.Annotations)
				t.Logf("Debug: LastFetchedImage=%s", component.Status.LastFetchedImage)
				for _, cond := range component.Status.Conditions {
					t.Logf("Debug: Condition %s=%s (Reason=%s, Message=%s)",
						cond.Type, cond.Status, cond.Reason, cond.Message)
				}

				counter, _ := mockClient.GetCounter(ctx)
				t.Logf("Debug: MockServer counter=%d", counter)

				statusCode, _, _ := mockClient.TestFetch(ctx)
				t.Logf("Debug: MockServer returns status=%d", statusCode)

				t.Fatalf("Expected ConfigurationRetrieved=True: %v", err)
			}
			t.Logf("Verified: ConfigurationRetrieved=True (recovered)")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return teardownNamespace(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// TestRealHTTP_NoFetchWithoutTrigger verifies that after initial success,
// the controller doesn't make new HTTP requests without a trigger (image change or force-refresh).
func TestRealHTTP_NoFetchWithoutTrigger(t *testing.T) {
	const componentName = "no-fetch-test-component"

	feature := features.New("no-fetch-without-trigger").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return setupNamespace(ctx, t, cfg)
		}).
		Assess("create component and wait for initial success", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			err := framework.NewScalityUIComponentBuilder(componentName, namespace).
				WithImage(framework.MockServerImage).
				Create(ctx, k8sClient)
			if err != nil {
				t.Fatalf("Failed to create ScalityUIComponent: %v", err)
			}
			t.Logf("Created ScalityUIComponent %s", componentName)

			if err := framework.WaitForDeploymentReady(ctx, k8sClient, namespace, componentName, framework.LongTimeout); err != nil {
				t.Fatalf("Deployment not ready: %v", err)
			}

			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionTrue, framework.LongTimeout); err != nil {
				t.Fatalf("Initial configuration not retrieved: %v", err)
			}
			t.Logf("Initial configuration retrieved successfully")

			return ctx
		}).
		Assess("reset counter and configure 500", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			if err := mockClient.Reset(ctx); err != nil {
				t.Fatalf("Failed to reset counter: %v", err)
			}

			if err := mockClient.SetStatusCode(ctx, 500); err != nil {
				t.Fatalf("Failed to set status code: %v", err)
			}
			t.Logf("Reset counter to 0 and configured mock server to return 500")

			return ctx
		}).
		Assess("verify no fetch without trigger", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			// Trigger multiple reconciles by updating labels (not image or force-refresh)
			for i := 0; i < 5; i++ {
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					component := &uiv1alpha1.ScalityUIComponent{}
					if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
						return err
					}
					if component.Labels == nil {
						component.Labels = make(map[string]string)
					}
					component.Labels["trigger-reconcile"] = fmt.Sprintf("value-%d", i)
					return k8sClient.Resources(namespace).Update(ctx, component)
				})
				if err != nil {
					t.Fatalf("Failed to update labels: %v", err)
				}
				time.Sleep(200 * time.Millisecond)
			}

			t.Logf("Triggered 5 reconciles via label updates, waiting 3 seconds...")
			time.Sleep(3 * time.Second)

			counter, err := mockClient.GetCounter(ctx)
			if err != nil {
				t.Fatalf("Failed to get counter: %v", err)
			}

			if counter != 0 {
				t.Fatalf("Expected counter=0 (no fetch without image change or force-refresh), got %d", counter)
			}
			t.Logf("Verified: No HTTP requests made (counter=0)")
			t.Logf("LastFetchedImage mechanism correctly prevents redundant fetches")

			return ctx
		}).
		Assess("verify condition still True despite mock returning 500", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)

			component := &uiv1alpha1.ScalityUIComponent{}
			if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
				t.Fatalf("Failed to get component: %v", err)
			}

			var condition *metav1.Condition
			for i := range component.Status.Conditions {
				if component.Status.Conditions[i].Type == framework.ConditionConfigurationRetrieved {
					condition = &component.Status.Conditions[i]
					break
				}
			}

			if condition == nil || condition.Status != metav1.ConditionTrue {
				t.Fatalf("Expected condition to still be True (no refetch happened)")
			}
			t.Logf("Verified: ConfigurationRetrieved=True (no refetch triggered)")

			return ctx
		}).
		Assess("force-refresh triggers fetch to failing server", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			k8sClient := cfg.Client()
			namespace := ctx.Value(realHTTPNamespaceKey).(string)
			mockClient := framework.NewMockServerClient(namespace, componentName)

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				component := &uiv1alpha1.ScalityUIComponent{}
				if err := k8sClient.Resources(namespace).Get(ctx, componentName, namespace, component); err != nil {
					return err
				}
				if component.Annotations == nil {
					component.Annotations = make(map[string]string)
				}
				component.Annotations[uiv1alpha1.ForceRefreshAnnotation] = "true"
				return k8sClient.Resources(namespace).Update(ctx, component)
			})
			if err != nil {
				t.Fatalf("Failed to set force-refresh annotation: %v", err)
			}
			t.Logf("Set force-refresh annotation")

			// Wait for the request to be made (counter should go from 0 to 1)
			if err := framework.WaitForMockServerCounter(ctx, mockClient, 1, framework.DefaultTimeout); err != nil {
				t.Fatalf("Fetch request not received after force-refresh: %v", err)
			}
			t.Logf("Force-refresh triggered HTTP request (counter=1)")

			// Verify condition becomes False (since server returns 500)
			if err := framework.WaitForScalityUIComponentCondition(ctx, k8sClient, namespace, componentName,
				framework.ConditionConfigurationRetrieved, metav1.ConditionFalse, framework.DefaultTimeout); err != nil {
				t.Fatalf("Expected ConfigurationRetrieved=False after 500 error: %v", err)
			}
			t.Logf("Verified: ConfigurationRetrieved=False after force-refresh to failing server")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return teardownNamespace(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func setupNamespace(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	k8sClient := cfg.Client()

	testNamespace := envconf.RandomName("real-http", 16)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	if err := k8sClient.Resources().Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace %s: %v", testNamespace, err)
	}
	t.Logf("Created namespace %s", testNamespace)

	ctx = context.WithValue(ctx, realHTTPNamespaceKey, testNamespace)
	return ctx
}

func teardownNamespace(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	k8sClient := cfg.Client()
	namespace := ctx.Value(realHTTPNamespaceKey).(string)

	mockClient := framework.NewMockServerClient(namespace, "")
	if err := mockClient.CleanupCurlPods(ctx); err != nil {
		t.Logf("Warning: Failed to cleanup curl pods: %v", err)
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := k8sClient.Resources().Delete(ctx, ns); err != nil {
		t.Logf("Warning: Failed to delete namespace %s: %v", namespace, err)
	} else {
		t.Logf("Deleted namespace %s", namespace)
	}

	return ctx
}
