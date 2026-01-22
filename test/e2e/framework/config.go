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
	"os"
	"runtime"
	"strings"
)

const (
	OperatorNamespace    = "ui-operator-system"
	OperatorDeployment   = "ui-operator-controller-manager"
	ControlPlaneLabel    = "control-plane"
	ControlPlaneValue    = "controller-manager"
	DefaultOperatorImage = "ui-operator:e2e"
)

func GetOperatorImage() string {
	if img := os.Getenv("E2E_OPERATOR_IMAGE"); img != "" {
		return img
	}
	return DefaultOperatorImage
}

func GetProjectRoot() string {
	if root := os.Getenv("E2E_PROJECT_ROOT"); root != "" {
		return root
	}
	return "../.."
}

// SkipBuild returns true if all local builds should be skipped.
// This affects both operator and mock server builds.
func SkipBuild() bool {
	val := strings.ToLower(os.Getenv("E2E_SKIP_BUILD"))
	return val == "true" || val == "1" || val == "yes"
}

// SkipOperatorBuild returns true if only the operator image build should be skipped.
// Use this when pulling a pre-built operator image from registry but still need to build mock server locally.
func SkipOperatorBuild() bool {
	val := strings.ToLower(os.Getenv("E2E_SKIP_OPERATOR_BUILD"))
	return val == "true" || val == "1" || val == "yes"
}

func SkipOperatorDeploy() bool {
	val := strings.ToLower(os.Getenv("E2E_SKIP_OPERATOR"))
	return val == "true" || val == "1" || val == "yes"
}

func GetTargetArch() string {
	if arch := os.Getenv("E2E_TARGET_ARCH"); arch != "" {
		return arch
	}
	return runtime.GOARCH
}
