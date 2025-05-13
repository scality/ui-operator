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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ScalityUIComponentSpec defines the desired state of ScalityUIComponent
type ScalityUIComponentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Image string `json:"image"`
}

// ScalityUIComponentStatus defines the observed state of ScalityUIComponent
type ScalityUIComponentStatus struct {
	// Path represents the URL path to the UI component
	PublicPath string `json:"publicPath,omitempty"`
	// Kind represents the type of UI component
	Kind string `json:"kind,omitempty"`
	// Version represents the version of the UI component
	Version string `json:"version,omitempty"`
	// Conditions represent the latest available observations of a UI component's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScalityUIComponent is the Schema for the scalityuicomponents API
type ScalityUIComponent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScalityUIComponentSpec   `json:"spec,omitempty"`
	Status ScalityUIComponentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScalityUIComponentList contains a list of ScalityUIComponent
type ScalityUIComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScalityUIComponent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScalityUIComponent{}, &ScalityUIComponentList{})
}
