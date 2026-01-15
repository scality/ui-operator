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
	"os"
	"os/exec"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const (
	MockServerImage       = "mock-server:e2e"
	MockServerPort        = 8080
	MockServerServicePort = 80
)

type MockServer struct {
	Name      string
	Namespace string
	client    klient.Client
}

func NewMockServer(name, namespace string, client klient.Client) *MockServer {
	return &MockServer{
		Name:      name,
		Namespace: namespace,
		client:    client,
	}
}

func (m *MockServer) Deploy(ctx context.Context) error {
	replicas := int32(1)
	labels := map[string]string{
		"app":                          m.Name,
		"app.kubernetes.io/managed-by": "e2e-test",
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "mock-server",
							Image:           MockServerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: MockServerPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(MockServerPort),
									},
								},
								InitialDelaySeconds: 1,
								PeriodSeconds:       2,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(MockServerPort),
									},
								},
								InitialDelaySeconds: 1,
								PeriodSeconds:       5,
							},
						},
					},
				},
			},
		},
	}

	if err := m.client.Resources(m.Namespace).Create(ctx, deployment); err != nil {
		return fmt.Errorf("failed to create mock server deployment: %w", err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Port:       MockServerServicePort,
					TargetPort: intstr.FromInt(MockServerPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := m.client.Resources(m.Namespace).Create(ctx, service); err != nil {
		return fmt.Errorf("failed to create mock server service: %w", err)
	}

	return nil
}

func (m *MockServer) WaitReady(ctx context.Context, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var deployment appsv1.Deployment
		if err := m.client.Resources(m.Namespace).Get(ctx, m.Name, m.Namespace, &deployment); err != nil {
			return false, nil
		}

		if deployment.Status.ReadyReplicas > 0 &&
			deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			return true, nil
		}
		return false, nil
	})
}

func (m *MockServer) Delete(ctx context.Context) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
	}
	if err := m.client.Resources(m.Namespace).Delete(ctx, deployment); err != nil {
		return fmt.Errorf("failed to delete mock server deployment: %w", err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
	}
	if err := m.client.Resources(m.Namespace).Delete(ctx, service); err != nil {
		return fmt.Errorf("failed to delete mock server service: %w", err)
	}

	return nil
}

func (m *MockServer) ServiceURL() string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", m.Name, m.Namespace, MockServerServicePort)
}

func (m *MockServer) ServiceName() string {
	return m.Name
}

func BuildAndLoadMockServerImage(projectRoot, clusterName string) error {
	mockServerDir := filepath.Join(projectRoot, "test", "e2e", "mock-server")

	fmt.Println("Building mock server image...")
	buildCmd := exec.Command("docker", "build", "-t", MockServerImage, ".")
	buildCmd.Dir = mockServerDir
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("failed to build mock server image: %w", err)
	}

	fmt.Println("Loading mock server image to Kind cluster...")
	loadCmd := exec.Command("kind", "load", "docker-image", MockServerImage, "--name", clusterName)
	loadCmd.Stdout = os.Stdout
	loadCmd.Stderr = os.Stderr
	if err := loadCmd.Run(); err != nil {
		return fmt.Errorf("failed to load mock server image to Kind: %w", err)
	}

	return nil
}

func BuildAndLoadMockServerSetup(clusterName string) func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		if SkipBuild() {
			fmt.Println("Skipping mock server build (E2E_SKIP_BUILD=true)")
			return ctx, nil
		}

		projectRoot, err := filepath.Abs(GetProjectRoot())
		if err != nil {
			return ctx, fmt.Errorf("failed to get project root: %w", err)
		}

		if err := BuildAndLoadMockServerImage(projectRoot, clusterName); err != nil {
			return ctx, err
		}

		return ctx, nil
	}
}
