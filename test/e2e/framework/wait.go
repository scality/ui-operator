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
	"fmt"
	"strings"
	"time"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/e2e-framework/klient"
)

const (
	DefaultPollInterval = time.Second
	DefaultTimeout      = 30 * time.Second
	LongTimeout         = 2 * time.Minute
)

const (
	ConditionConfigurationRetrieved = "ConfigurationRetrieved"
	ConditionDependenciesReady      = "DependenciesReady"
	ConditionConfigMapReady         = "ConfigMapReady"
	ConditionDeploymentReady        = "DeploymentReady"

	RuntimeConfigMapSuffix = "-runtime-app-configuration"
	ConfigVolumePrefix     = "config-volume-"
)

func WaitForCRDEstablished(ctx context.Context, client klient.Client, crdName string) error {
	return wait.PollUntilContextTimeout(ctx, DefaultPollInterval, DefaultTimeout, true, func(ctx context.Context) (bool, error) {
		var crd apiextensionsv1.CustomResourceDefinition
		if err := client.Resources().Get(ctx, crdName, "", &crd); err != nil {
			return false, nil
		}

		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func WaitForScalityUIReady(ctx context.Context, client klient.Client, name string, timeout time.Duration) error {
	var lastPhase string
	var lastConditions string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var ui uiv1alpha1.ScalityUI
		if err := client.Resources().Get(ctx, name, "", &ui); err != nil {
			return false, nil
		}

		lastPhase = ui.Status.Phase
		lastConditions = formatConditions(ui.Status.Conditions)

		if ui.Status.Phase == uiv1alpha1.PhaseReady {
			return true, nil
		}

		for _, cond := range ui.Status.Conditions {
			if cond.Type == uiv1alpha1.ConditionTypeReady && cond.Status == metav1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("ScalityUI %s not ready (phase=%s, conditions=%s): %w", name, lastPhase, lastConditions, err)
	}
	return nil
}

func WaitForScalityUIComponentConfigured(ctx context.Context, client klient.Client, namespace, name string, timeout time.Duration) error {
	var lastConditions string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var component uiv1alpha1.ScalityUIComponent
		if err := client.Resources(namespace).Get(ctx, name, namespace, &component); err != nil {
			return false, nil
		}

		lastConditions = formatConditions(component.Status.Conditions)

		for _, cond := range component.Status.Conditions {
			if cond.Type == ConditionConfigurationRetrieved && cond.Status == metav1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("ScalityUIComponent %s/%s not configured (conditions=%s): %w", namespace, name, lastConditions, err)
	}
	return nil
}

func WaitForScalityUIComponentExposerReady(ctx context.Context, client klient.Client, namespace, name string, timeout time.Duration) error {
	var lastConditions string
	var missingConditions []string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var exposer uiv1alpha1.ScalityUIComponentExposer
		if err := client.Resources(namespace).Get(ctx, name, namespace, &exposer); err != nil {
			return false, nil
		}

		lastConditions = formatConditions(exposer.Status.Conditions)

		requiredConditions := map[string]bool{
			ConditionDependenciesReady: false,
			ConditionConfigMapReady:    false,
			ConditionDeploymentReady:   false,
		}

		for _, cond := range exposer.Status.Conditions {
			if _, exists := requiredConditions[cond.Type]; exists {
				if cond.Status == metav1.ConditionTrue {
					requiredConditions[cond.Type] = true
				}
			}
		}

		missingConditions = nil
		for condType, ready := range requiredConditions {
			if !ready {
				missingConditions = append(missingConditions, condType)
			}
		}

		return len(missingConditions) == 0, nil
	})

	if err != nil {
		return fmt.Errorf("ScalityUIComponentExposer %s/%s not ready (missing=%v, conditions=%s): %w",
			namespace, name, missingConditions, lastConditions, err)
	}
	return nil
}

func WaitForServiceExists(ctx context.Context, client klient.Client, namespace, name string, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var service corev1.Service
		if err := client.Resources(namespace).Get(ctx, name, namespace, &service); err != nil {
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("Service %s/%s not found: %w", namespace, name, err)
	}
	return nil
}

func WaitForConfigMapExists(ctx context.Context, client klient.Client, namespace, name string, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var cm corev1.ConfigMap
		if err := client.Resources(namespace).Get(ctx, name, namespace, &cm); err != nil {
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("ConfigMap %s/%s not found: %w", namespace, name, err)
	}
	return nil
}

func WaitForDeploymentHasVolume(ctx context.Context, client klient.Client, namespace, name, volumeName string, timeout time.Duration) error {
	var foundVolumes []string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var deployment appsv1.Deployment
		if err := client.Resources(namespace).Get(ctx, name, namespace, &deployment); err != nil {
			return false, nil
		}

		foundVolumes = nil
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			foundVolumes = append(foundVolumes, vol.Name)
			if vol.Name == volumeName {
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("Deployment %s/%s missing volume %s (found=%v): %w", namespace, name, volumeName, foundVolumes, err)
	}
	return nil
}

func WaitForNamespaceDeleted(ctx context.Context, client klient.Client, name string, timeout time.Duration) error {
	var lastPhase corev1.NamespacePhase

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var ns corev1.Namespace
		if err := client.Resources().Get(ctx, name, "", &ns); err != nil {
			return true, nil
		}
		lastPhase = ns.Status.Phase
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("Namespace %s not deleted (phase=%s): %w", name, lastPhase, err)
	}
	return nil
}

func GetScalityUIComponent(ctx context.Context, client klient.Client, namespace, name string) (*uiv1alpha1.ScalityUIComponent, error) {
	var component uiv1alpha1.ScalityUIComponent
	if err := client.Resources(namespace).Get(ctx, name, namespace, &component); err != nil {
		return nil, fmt.Errorf("failed to get ScalityUIComponent %s/%s: %w", namespace, name, err)
	}
	return &component, nil
}

func DeleteScalityUI(ctx context.Context, client klient.Client, name string) error {
	ui := &uiv1alpha1.ScalityUI{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return client.Resources().Delete(ctx, ui)
}

func formatConditions(conditions []metav1.Condition) string {
	if len(conditions) == 0 {
		return "none"
	}
	var parts []string
	for _, c := range conditions {
		parts = append(parts, fmt.Sprintf("%s=%s", c.Type, c.Status))
	}
	return strings.Join(parts, ",")
}
