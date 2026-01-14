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

package framework

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

func AddToScheme(scheme *runtime.Scheme) error {
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := uiv1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	return nil
}

func SetupScheme() func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		if err := AddToScheme(cfg.Client().Resources().GetScheme()); err != nil {
			return ctx, err
		}
		return ctx, nil
	}
}
