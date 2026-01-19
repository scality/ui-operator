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

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient"
)

type ScalityUIComponentBuilder struct {
	name      string
	namespace string
	image     string
	mountPath string
	labels    map[string]string
}

func NewScalityUIComponentBuilder(name, namespace string) *ScalityUIComponentBuilder {
	return &ScalityUIComponentBuilder{
		name:      name,
		namespace: namespace,
		mountPath: "/app/config",
		labels:    make(map[string]string),
	}
}

func (b *ScalityUIComponentBuilder) WithImage(image string) *ScalityUIComponentBuilder {
	b.image = image
	return b
}

func (b *ScalityUIComponentBuilder) WithMountPath(mountPath string) *ScalityUIComponentBuilder {
	b.mountPath = mountPath
	return b
}

func (b *ScalityUIComponentBuilder) WithLabel(key, value string) *ScalityUIComponentBuilder {
	b.labels[key] = value
	return b
}

func (b *ScalityUIComponentBuilder) Build() *uiv1alpha1.ScalityUIComponent {
	component := &uiv1alpha1.ScalityUIComponent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.name,
			Namespace: b.namespace,
		},
		Spec: uiv1alpha1.ScalityUIComponentSpec{
			Image:     b.image,
			MountPath: b.mountPath,
		},
	}

	if len(b.labels) > 0 {
		component.Labels = b.labels
	}

	return component
}

func (b *ScalityUIComponentBuilder) Create(ctx context.Context, client klient.Client) error {
	component := b.Build()
	return client.Resources(b.namespace).Create(ctx, component)
}

func DeleteScalityUIComponent(ctx context.Context, client klient.Client, namespace, name string) error {
	component := &uiv1alpha1.ScalityUIComponent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return client.Resources(namespace).Delete(ctx, component)
}
