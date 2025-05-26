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

// ScalityUIComponentExposerSpec defines the desired state of ScalityUIComponentExposer
type ScalityUIComponentExposerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ScalityUI references the ScalityUI resource name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ScalityUI string `json:"scalityUI"`

	// ScalityUIComponent references the ScalityUIComponent resource name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ScalityUIComponent string `json:"scalityUIComponent"`

	// AppHistoryPath specifies the path to the app history
	// +kubebuilder:validation:Required
	AppHistoryPath string `json:"appHistoryPath"`

	// Auth contains authentication configuration for the exposed component
	// +kubebuilder:validation:Optional
	Auth *AuthConfig `json:"auth,omitempty"`
}

// AuthConfig defines authentication configuration
type AuthConfig struct {
	// Kind specifies the authentication type (e.g., "OIDC")
	// +kubebuilder:validation:Enum=OIDC;Basic;None
	// +kubebuilder:default="OIDC"
	Kind string `json:"kind,omitempty"`

	// ProviderURL is the OIDC provider URL
	ProviderURL string `json:"providerUrl,omitempty"`

	// RedirectURL is the redirect URL after authentication
	// +kubebuilder:default="/"
	RedirectURL string `json:"redirectUrl,omitempty"`

	// ClientID is the OIDC client ID
	ClientID string `json:"clientId,omitempty"`

	// ResponseType specifies the OIDC response type
	// +kubebuilder:default="code"
	ResponseType string `json:"responseType,omitempty"`

	// Scopes specifies the OIDC scopes
	// +kubebuilder:default="openid email profile"
	Scopes string `json:"scopes,omitempty"`

	// ProviderLogout enables provider logout
	// +kubebuilder:default=true
	ProviderLogout *bool `json:"providerLogout,omitempty"`
}

// ScalityUIComponentExposerStatus defines the observed state of ScalityUIComponentExposer
type ScalityUIComponentExposerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ConfigMapName represents the name of the created ConfigMap
	RuntimeAppConfigurationConfigMapName string `json:"runtimeAppConfigurationConfigMapName,omitempty"`

	// Conditions represent the latest available observations of the exposer's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScalityUIComponentExposer is the Schema for the scalityuicomponentexposers API
type ScalityUIComponentExposer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScalityUIComponentExposerSpec   `json:"spec,omitempty"`
	Status ScalityUIComponentExposerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScalityUIComponentExposerList contains a list of ScalityUIComponentExposer
type ScalityUIComponentExposerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScalityUIComponentExposer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScalityUIComponentExposer{}, &ScalityUIComponentExposerList{})
}
