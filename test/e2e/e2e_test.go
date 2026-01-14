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
