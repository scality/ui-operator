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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
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

func WaitForScalityUIComponentCondition(ctx context.Context, client klient.Client,
	namespace, name string, conditionType string, expectedStatus metav1.ConditionStatus,
	timeout time.Duration) error {

	var lastConditions string
	var lastReason string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var component uiv1alpha1.ScalityUIComponent
		if err := client.Resources(namespace).Get(ctx, name, namespace, &component); err != nil {
			return false, nil
		}

		lastConditions = formatConditions(component.Status.Conditions)

		for _, cond := range component.Status.Conditions {
			if cond.Type == conditionType {
				lastReason = cond.Reason
				if cond.Status == expectedStatus {
					return true, nil
				}
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("ScalityUIComponent %s/%s condition %s not %s (reason=%s, conditions=%s): %w",
			namespace, name, conditionType, expectedStatus, lastReason, lastConditions, err)
	}
	return nil
}

func WaitForScalityUIComponentAnnotationAbsent(ctx context.Context, client klient.Client,
	namespace, name, annotationKey string, timeout time.Duration) error {

	var lastAnnotations map[string]string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var component uiv1alpha1.ScalityUIComponent
		if err := client.Resources(namespace).Get(ctx, name, namespace, &component); err != nil {
			return false, nil
		}

		lastAnnotations = component.Annotations
		if component.Annotations == nil {
			return true, nil
		}

		_, exists := component.Annotations[annotationKey]
		return !exists, nil
	})

	if err != nil {
		return fmt.Errorf("ScalityUIComponent %s/%s annotation %s still present (annotations=%v): %w",
			namespace, name, annotationKey, lastAnnotations, err)
	}
	return nil
}

func WaitForMockServerCounter(ctx context.Context, mockClient *MockServerClient,
	expected int64, timeout time.Duration) error {

	var lastCounter int64
	var lastErr error

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		counter, err := mockClient.GetCounter(ctx)
		if err != nil {
			lastErr = err
			return false, nil
		}
		lastErr = nil
		lastCounter = counter
		return counter >= expected, nil
	})

	if err != nil {
		if lastErr != nil {
			return fmt.Errorf("mock server counter did not reach %d (last=%d, lastErr=%v): %w", expected, lastCounter, lastErr, err)
		}
		return fmt.Errorf("mock server counter did not reach %d (last=%d): %w", expected, lastCounter, err)
	}
	return nil
}

// GetDeploymentReplicaSets returns all ReplicaSets owned by a Deployment
func GetDeploymentReplicaSets(ctx context.Context, client klient.Client, namespace, deploymentName string) ([]appsv1.ReplicaSet, error) {
	var deployment appsv1.Deployment
	if err := client.Resources(namespace).Get(ctx, deploymentName, namespace, &deployment); err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, deploymentName, err)
	}

	var rsList appsv1.ReplicaSetList
	if err := client.Resources(namespace).List(ctx, &rsList); err != nil {
		return nil, fmt.Errorf("failed to list replicasets in %s: %w", namespace, err)
	}

	var owned []appsv1.ReplicaSet
	for _, rs := range rsList.Items {
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" && ref.Name == deploymentName {
				owned = append(owned, rs)
				break
			}
		}
	}
	return owned, nil
}

// GetActiveReplicaSet returns the only active ReplicaSet (replicas > 0) for a Deployment
// Returns error if there are multiple active ReplicaSets (rolling update in progress)
func GetActiveReplicaSet(ctx context.Context, client klient.Client, namespace, deploymentName string) (*appsv1.ReplicaSet, error) {
	rsList, err := GetDeploymentReplicaSets(ctx, client, namespace, deploymentName)
	if err != nil {
		return nil, err
	}

	var activeRS []appsv1.ReplicaSet
	for _, rs := range rsList {
		if rs.Status.Replicas > 0 {
			activeRS = append(activeRS, rs)
		}
	}

	if len(activeRS) == 0 {
		return nil, fmt.Errorf("no active ReplicaSet found for %s/%s", namespace, deploymentName)
	}
	if len(activeRS) > 1 {
		var names []string
		for _, rs := range activeRS {
			names = append(names, fmt.Sprintf("%s(replicas=%d)", rs.Name, rs.Status.Replicas))
		}
		return nil, fmt.Errorf("multiple active ReplicaSets for %s/%s: %v (rolling update in progress)", namespace, deploymentName, names)
	}

	return &activeRS[0], nil
}

// WaitForDeploymentStable waits for a Deployment to have exactly one active ReplicaSet
func WaitForDeploymentStable(ctx context.Context, client klient.Client, namespace, deploymentName string, timeout time.Duration) (*appsv1.ReplicaSet, error) {
	var activeRS *appsv1.ReplicaSet
	var lastErr error

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		rs, err := GetActiveReplicaSet(ctx, client, namespace, deploymentName)
		if err != nil {
			lastErr = err
			return false, nil
		}
		activeRS = rs
		lastErr = nil
		return true, nil
	})

	if err != nil {
		if lastErr != nil {
			return nil, fmt.Errorf("deployment %s/%s not stable: %v: %w", namespace, deploymentName, lastErr, err)
		}
		return nil, fmt.Errorf("deployment %s/%s not stable: %w", namespace, deploymentName, err)
	}
	return activeRS, nil
}

// WaitForNewReplicaSet waits for a new ReplicaSet to be created for a Deployment
func WaitForNewReplicaSet(ctx context.Context, client klient.Client,
	namespace, deploymentName string, excludeNames []string, timeout time.Duration) (string, error) {

	excludeSet := make(map[string]bool)
	for _, name := range excludeNames {
		excludeSet[name] = true
	}

	var newRSName string
	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		rsList, err := GetDeploymentReplicaSets(ctx, client, namespace, deploymentName)
		if err != nil {
			return false, nil
		}

		for _, rs := range rsList {
			// Only consider active ReplicaSets (replicas > 0) to avoid returning old scaled-down RS
			if !excludeSet[rs.Name] && rs.Status.Replicas > 0 {
				newRSName = rs.Name
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		return "", fmt.Errorf("new ReplicaSet not created for %s/%s (excluded=%v): %w",
			namespace, deploymentName, excludeNames, err)
	}
	return newRSName, nil
}

// WaitForReplicaSetScaledDown waits for a ReplicaSet to have 0 replicas
func WaitForReplicaSetScaledDown(ctx context.Context, client klient.Client,
	namespace, rsName string, timeout time.Duration) error {

	var lastReplicas int32

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var rs appsv1.ReplicaSet
		if err := client.Resources(namespace).Get(ctx, rsName, namespace, &rs); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		}

		lastReplicas = rs.Status.Replicas
		return rs.Status.Replicas == 0, nil
	})

	if err != nil {
		return fmt.Errorf("ReplicaSet %s/%s not scaled down (replicas=%d): %w",
			namespace, rsName, lastReplicas, err)
	}
	return nil
}

// ResourceRef identifies a Kubernetes resource for version tracking
type ResourceRef struct {
	Kind      string
	Namespace string
	Name      string
}

// GetResourceVersion gets the ResourceVersion of a single resource
func GetResourceVersion(ctx context.Context, client klient.Client, ref ResourceRef) (string, error) {
	switch ref.Kind {
	case "Deployment":
		var obj appsv1.Deployment
		if err := client.Resources(ref.Namespace).Get(ctx, ref.Name, ref.Namespace, &obj); err != nil {
			return "", err
		}
		return obj.ResourceVersion, nil
	case "Service":
		var obj corev1.Service
		if err := client.Resources(ref.Namespace).Get(ctx, ref.Name, ref.Namespace, &obj); err != nil {
			return "", err
		}
		return obj.ResourceVersion, nil
	case "ConfigMap":
		var obj corev1.ConfigMap
		if err := client.Resources(ref.Namespace).Get(ctx, ref.Name, ref.Namespace, &obj); err != nil {
			return "", err
		}
		return obj.ResourceVersion, nil
	default:
		return "", fmt.Errorf("unsupported resource kind: %s", ref.Kind)
	}
}

// GetResourceVersions gets ResourceVersions for multiple resources
func GetResourceVersions(ctx context.Context, client klient.Client, refs []ResourceRef) (map[string]string, error) {
	result := make(map[string]string)
	for _, ref := range refs {
		key := fmt.Sprintf("%s/%s/%s", ref.Kind, ref.Namespace, ref.Name)
		version, err := GetResourceVersion(ctx, client, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to get version for %s: %w", key, err)
		}
		result[key] = version
	}
	return result, nil
}

// DeleteOperatorPod deletes the operator pod and waits for a new one to be running
func DeleteOperatorPod(ctx context.Context, client klient.Client, timeout time.Duration) error {
	var pods corev1.PodList
	if err := client.Resources(OperatorNamespace).List(ctx, &pods); err != nil {
		return fmt.Errorf("failed to list pods in %s: %w", OperatorNamespace, err)
	}

	var operatorPod *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if val, ok := pod.Labels[ControlPlaneLabel]; ok && val == ControlPlaneValue {
			operatorPod = pod
			break
		}
	}

	if operatorPod == nil {
		return fmt.Errorf("operator pod not found in %s", OperatorNamespace)
	}

	oldPodName := operatorPod.Name

	if err := client.Resources(OperatorNamespace).Delete(ctx, operatorPod); err != nil {
		return fmt.Errorf("failed to delete operator pod %s: %w", oldPodName, err)
	}

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		if err := client.Resources(OperatorNamespace).List(ctx, &pods); err != nil {
			return false, nil
		}

		for i := range pods.Items {
			pod := &pods.Items[i]
			if val, ok := pod.Labels[ControlPlaneLabel]; ok && val == ControlPlaneValue {
				if pod.Name != oldPodName && pod.Status.Phase == corev1.PodRunning {
					allReady := true
					for _, cs := range pod.Status.ContainerStatuses {
						if !cs.Ready {
							allReady = false
							break
						}
					}
					if allReady {
						return true, nil
					}
				}
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("new operator pod not running after deleting %s: %w", oldPodName, err)
	}
	return nil
}

// GetDeploymentPodTemplateHash gets the hash annotation from a Deployment's pod template
func GetDeploymentPodTemplateHash(ctx context.Context, client klient.Client,
	namespace, name, annotationKey string) (string, error) {

	var deployment appsv1.Deployment
	if err := client.Resources(namespace).Get(ctx, name, namespace, &deployment); err != nil {
		return "", fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	if deployment.Spec.Template.Annotations == nil {
		return "", nil
	}
	return deployment.Spec.Template.Annotations[annotationKey], nil
}

// WaitForDeploymentAnnotationChange waits for a Deployment's pod template annotation to change
func WaitForDeploymentAnnotationChange(ctx context.Context, client klient.Client,
	namespace, name, annotationKey, oldValue string, timeout time.Duration) (string, error) {

	var newValue string
	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var deployment appsv1.Deployment
		if err := client.Resources(namespace).Get(ctx, name, namespace, &deployment); err != nil {
			return false, nil
		}

		if deployment.Spec.Template.Annotations == nil {
			return false, nil
		}

		newValue = deployment.Spec.Template.Annotations[annotationKey]
		return newValue != "" && newValue != oldValue, nil
	})

	if err != nil {
		return "", fmt.Errorf("deployment %s/%s annotation %s did not change from %s: %w",
			namespace, name, annotationKey, oldValue, err)
	}
	return newValue, nil
}

// WaitForDeploymentNoVolume waits for a Deployment to NOT have the specified volume
func WaitForDeploymentNoVolume(ctx context.Context, client klient.Client,
	namespace, name, volumeName string, timeout time.Duration) error {

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
				return false, nil
			}
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("Deployment %s/%s still has volume %s (found=%v): %w",
			namespace, name, volumeName, foundVolumes, err)
	}
	return nil
}

// WaitForConfigMapDeleted waits for a ConfigMap to be deleted
func WaitForConfigMapDeleted(ctx context.Context, client klient.Client,
	namespace, name string, timeout time.Duration) error {

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var cm corev1.ConfigMap
		if err := client.Resources(namespace).Get(ctx, name, namespace, &cm); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("ConfigMap %s/%s was not deleted: %w", namespace, name, err)
	}
	return nil
}

// GetConfigMapFinalizers gets the finalizers from a ConfigMap
func GetConfigMapFinalizers(ctx context.Context, client klient.Client,
	namespace, name string) ([]string, error) {

	var cm corev1.ConfigMap
	if err := client.Resources(namespace).Get(ctx, name, namespace, &cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, name, err)
	}
	return cm.Finalizers, nil
}

// WaitForConfigMapHasFinalizer waits for a ConfigMap to have a specific finalizer
func WaitForConfigMapHasFinalizer(ctx context.Context, client klient.Client,
	namespace, name, finalizer string, timeout time.Duration) error {

	var lastFinalizers []string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var cm corev1.ConfigMap
		if err := client.Resources(namespace).Get(ctx, name, namespace, &cm); err != nil {
			return false, nil
		}

		lastFinalizers = cm.Finalizers
		for _, f := range cm.Finalizers {
			if f == finalizer {
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("ConfigMap %s/%s does not have finalizer %s (found=%v): %w",
			namespace, name, finalizer, lastFinalizers, err)
	}
	return nil
}

// WaitForConfigMapNoFinalizer waits for a ConfigMap to NOT have a specific finalizer
func WaitForConfigMapNoFinalizer(ctx context.Context, client klient.Client,
	namespace, name, finalizer string, timeout time.Duration) error {

	var lastFinalizers []string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var cm corev1.ConfigMap
		if err := client.Resources(namespace).Get(ctx, name, namespace, &cm); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		}

		lastFinalizers = cm.Finalizers
		for _, f := range cm.Finalizers {
			if f == finalizer {
				return false, nil
			}
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("ConfigMap %s/%s still has finalizer %s (found=%v): %w",
			namespace, name, finalizer, lastFinalizers, err)
	}
	return nil
}

// WaitForDeploymentHasVolumeMount waits for a Deployment container to have a specific volume mount
func WaitForDeploymentHasVolumeMount(ctx context.Context, client klient.Client,
	namespace, deploymentName, volumeName, expectedMountPath string, timeout time.Duration) error {

	var foundMounts []string

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var deployment appsv1.Deployment
		if err := client.Resources(namespace).Get(ctx, deploymentName, namespace, &deployment); err != nil {
			return false, nil
		}

		foundMounts = nil
		for _, container := range deployment.Spec.Template.Spec.Containers {
			for _, mount := range container.VolumeMounts {
				foundMounts = append(foundMounts, fmt.Sprintf("%s->%s", mount.Name, mount.MountPath))
				if mount.Name == volumeName && mount.MountPath == expectedMountPath {
					return true, nil
				}
			}
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("Deployment %s/%s missing volume mount %s at %s (found=%v): %w",
			namespace, deploymentName, volumeName, expectedMountPath, foundMounts, err)
	}
	return nil
}
