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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

func BuildOperatorImage(projectRoot, image string) error {
	arch := GetTargetArch()
	fmt.Printf("Building operator binary for linux/%s...\n", arch)

	buildCmd := exec.Command("go", "build", "-o", "bin/manager", "cmd/main.go")
	buildCmd.Dir = projectRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	buildCmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		fmt.Sprintf("GOARCH=%s", arch),
	)
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("failed to build binary: %w", err)
	}

	fmt.Println("Building Docker image with pre-built binary...")
	dockerCmd := exec.Command("docker", "build",
		"-f", "test/e2e/Dockerfile.e2e",
		"-t", image,
		"bin",
	)
	dockerCmd.Dir = projectRoot
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	return dockerCmd.Run()
}

func LoadImageToKind(clusterName, image string) error {
	cmd := exec.Command("kind", "load", "docker-image", image, "--name", clusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func DeployOperator(projectRoot, image string) error {
	cmd := exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", image))
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func UndeployOperator(projectRoot string) error {
	cmd := exec.Command("make", "undeploy", "ignore-not-found=true")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func WaitForDeploymentReady(ctx context.Context, client klient.Client, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var deployment appsv1.Deployment
		if err := client.Resources(namespace).Get(ctx, name, namespace, &deployment); err != nil {
			return false, nil
		}

		if deployment.Spec.Replicas == nil {
			return false, nil
		}

		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas &&
			deployment.Status.UpdatedReplicas == *deployment.Spec.Replicas &&
			deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
			return true, nil
		}
		return false, nil
	})
}

func WaitForPodRunning(ctx context.Context, client klient.Client, namespace string, labelKey, labelValue string, timeout time.Duration) (*corev1.Pod, error) {
	var foundPod *corev1.Pod

	err := wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		if err := client.Resources(namespace).List(ctx, &pods); err != nil {
			return false, nil
		}

		for i := range pods.Items {
			pod := &pods.Items[i]
			if val, ok := pod.Labels[labelKey]; ok && val == labelValue {
				if pod.Status.Phase == corev1.PodRunning {
					allReady := true
					for _, cs := range pod.Status.ContainerStatuses {
						if !cs.Ready {
							allReady = false
							break
						}
					}
					if allReady {
						foundPod = pod
						return true, nil
					}
				}
			}
		}
		return false, nil
	})

	return foundPod, err
}

func GetPodLogs(ctx context.Context, client klient.Client, namespace, podName, containerName string) (string, error) {
	config := client.RESTConfig()
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", err
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
	})

	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(podLogs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Ensure rest package is used
var _ rest.Config

type clusterNameKey struct{}

func SetClusterName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, clusterNameKey{}, name)
}

func GetClusterName(ctx context.Context) string {
	if name, ok := ctx.Value(clusterNameKey{}).(string); ok {
		return name
	}
	return ""
}

func DeployOperatorSetup(clusterName string) func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		if SkipOperatorDeploy() {
			fmt.Println("Skipping operator deployment (E2E_SKIP_OPERATOR=true)")
			return SetClusterName(ctx, clusterName), nil
		}

		projectRoot, err := filepath.Abs(GetProjectRoot())
		if err != nil {
			return ctx, fmt.Errorf("failed to get project root: %w", err)
		}

		image := GetOperatorImage()

		if SkipBuild() || SkipOperatorBuild() {
			fmt.Printf("Skipping operator image build, using existing image: %s\n", image)
		} else {
			fmt.Printf("Building operator image: %s\n", image)
			if err := BuildOperatorImage(projectRoot, image); err != nil {
				return ctx, fmt.Errorf("failed to build operator image: %w", err)
			}
		}

		fmt.Printf("Loading image to Kind cluster: %s\n", clusterName)
		if err := LoadImageToKind(clusterName, image); err != nil {
			return ctx, fmt.Errorf("failed to load image to Kind: %w", err)
		}

		fmt.Printf("Deploying operator with image: %s\n", image)
		if err := DeployOperator(projectRoot, image); err != nil {
			return ctx, fmt.Errorf("failed to deploy operator: %w", err)
		}

		ctx = SetClusterName(ctx, clusterName)
		return ctx, nil
	}
}

func UndeployOperatorTeardown() func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		if SkipOperatorDeploy() {
			fmt.Println("Skipping operator undeploy (E2E_SKIP_OPERATOR=true)")
			return ctx, nil
		}

		projectRoot, err := filepath.Abs(GetProjectRoot())
		if err != nil {
			return ctx, fmt.Errorf("failed to get project root: %w", err)
		}

		fmt.Println("Undeploying operator...")
		if err := UndeployOperator(projectRoot); err != nil {
			fmt.Printf("Warning: failed to undeploy operator: %v\n", err)
		}

		return ctx, nil
	}
}
