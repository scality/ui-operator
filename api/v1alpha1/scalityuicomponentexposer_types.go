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

// ScalityUIComponentExposerSpec defines the desired state of ScalityUIComponentExposer
type ScalityUIComponentExposerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ScalityUI is the name of the ScalityUI resource
	ScalityUI string `json:"scality-ui"`

	// ScalityUIComponent is the name of the ScalityUIComponent resource
	ScalityUIComponent string `json:"scality-ui-component"`

	// RuntimeAppConfiguration contains the runtime configuration for the UI component
	RuntimeAppConfiguration *RuntimeAppConfiguration `json:"runtimeAppConfiguration,omitempty"`

	// Networks contains network configuration for the UI component exposer
	Networks *Networks `json:"networks,omitempty"`

	// Auth contains authentication configuration for the UI component
	Auth *Auth `json:"auth,omitempty"`

	// AppHistoryBasePath is the base path for app history
	AppHistoryBasePath string `json:"appHistoryBasePath,omitempty"`
}

// RuntimeAppConfiguration defines the runtime configuration for the UI component
type RuntimeAppConfiguration struct {
	// SelfConfiguration contains the self-configuration for the UI component
	SelfConfiguration SelfConfiguration `json:"selfConfiguration,omitempty"`
}

// SelfConfiguration defines the self-configuration for the UI component
type SelfConfiguration struct {
	// URL is the URL of the UI component
	URL string `json:"url,omitempty"`
}

// Networks defines the network configuration for the UI component exposer
type Networks struct {
	// Host is the hostname for the UI component
	Host string `json:"host,omitempty"`

	// TLS contains TLS configuration for the UI component
	TLS []TLSConfig `json:"tls,omitempty"`

	// IngressAnnotations contains annotations for the ingress
	IngressAnnotations map[string]string `json:"ingressAnnotations,omitempty"`
}

// TLSConfig defines the TLS configuration for the UI component
type TLSConfig struct {
	// Hosts is a list of hosts included in the TLS certificate
	Hosts []string `json:"hosts,omitempty"`

	// SecretName is the name of the TLS secret
	SecretName string `json:"secretName,omitempty"`
}

// Auth defines the authentication configuration for the UI component
type Auth struct {
	// Kind is the authentication kind (e.g., OIDC)
	Kind string `json:"kind,omitempty"`

	// ProviderURL is the URL of the authentication provider
	ProviderURL string `json:"providerUrl,omitempty"`

	// RedirectURL is the URL to redirect to after authentication
	RedirectURL string `json:"redirectUrl,omitempty"`

	// ClientID is the client ID for authentication
	ClientID string `json:"clientId,omitempty"`

	// ResponseType is the response type for authentication
	ResponseType string `json:"responseType,omitempty"`

	// Scopes are the scopes for authentication
	Scopes string `json:"scopes,omitempty"`

	// ProviderLogout indicates whether to use provider logout
	ProviderLogout bool `json:"providerLogout,omitempty"`
}

// ScalityUIComponentExposerStatus defines the observed state of ScalityUIComponentExposer
type ScalityUIComponentExposerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Kind is the component kind from the micro-app-configuration
	Kind string `json:"kind,omitempty"`

	// PublicPath is the public path from the micro-app-configuration
	PublicPath string `json:"publicPath,omitempty"`

	// Version is the version from the micro-app-configuration
	Version string `json:"version,omitempty"`

	// Conditions represent the latest available observations of an object's state
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
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
