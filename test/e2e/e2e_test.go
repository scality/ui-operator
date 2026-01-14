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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

func TestKubernetesConnection(t *testing.T) {
	feature := features.New("cluster-connection").
		Assess("can list namespaces", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client := cfg.Client()

			var namespaces corev1.NamespaceList
			if err := client.Resources().List(ctx, &namespaces); err != nil {
				t.Fatalf("failed to list namespaces: %v", err)
			}

			t.Logf("Found %d namespaces", len(namespaces.Items))

			if len(namespaces.Items) == 0 {
				t.Fatal("expected at least one namespace")
			}

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
