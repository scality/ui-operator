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
	"path/filepath"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"sigs.k8s.io/e2e-framework/klient"
)

func LoadYAMLWithTemplate(path string, values map[string]string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	tmpl, err := template.New(filepath.Base(path)).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", path, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return nil, fmt.Errorf("failed to execute template %s: %w", path, err)
	}

	return buf.Bytes(), nil
}

func LoadAndApplyYAML(ctx context.Context, client klient.Client, path string, namespace string) error {
	values := map[string]string{
		"namespace": namespace,
	}

	content, err := LoadYAMLWithTemplate(path, values)
	if err != nil {
		return err
	}

	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err = dec.Decode(content, nil, obj)
	if err != nil {
		return fmt.Errorf("failed to decode YAML %s: %w", path, err)
	}

	if obj.GetNamespace() == "" && namespace != "" {
		if obj.GetKind() != "ScalityUI" {
			obj.SetNamespace(namespace)
		}
	}

	if err := client.Resources(obj.GetNamespace()).Create(ctx, obj); err != nil {
		return fmt.Errorf("failed to create resource from %s: %w", path, err)
	}

	return nil
}

func GetTestDataPath(subpath string) string {
	projectRoot := GetProjectRoot()
	return filepath.Join(projectRoot, "test", "e2e", "testdata", subpath)
}
