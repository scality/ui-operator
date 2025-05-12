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

// ScalityUISpec defines the desired state of ScalityUI
type ScalityUISpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Image                 string `json:"image"`
	ProductName           string `json:"productName"`
	MountPath             string `json:"mountPath,omitempty"`
	CanChangeTheme        bool   `json:"canChangeTheme,omitempty"`
	CanChangeInstanceName bool   `json:"canChangeInstanceName,omitempty"`
	CanChangeLanguage     bool   `json:"canChangeLanguage,omitempty"`
	Favicon               string `json:"favicon,omitempty"`
	Logo                  string `json:"logo,omitempty"`
	Themes                Themes `json:"themes,omitempty"`
}

// Themes defines the various themes supported by the UI.
type Themes struct {
	Light Theme `json:"light"`
	Dark  Theme `json:"dark"`
}

// Theme defines the configuration of an individual theme.
type Theme struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	LogoPath string `json:"logoPath"`
}

// Navbar configures the UI navbar.
type Navbar struct {
	Main     []NavbarItem `json:"main"`
	SubLogin []NavbarItem `json:"subLogin"`
}

// NavbarItem defines an item in the navbar.
type NavbarItem struct {
	Kind       string            `json:"kind"`
	View       string            `json:"view"`
	Groups     []string          `json:"groups,omitempty"`
	Icon       string            `json:"icon,omitempty"`
	IsExternal bool              `json:"isExternal,omitempty"`
	Label      map[string]string `json:"label,omitempty"`
	URL        string            `json:"url,omitempty"`
}

// Networks configures network parameters for the UI.
type Networks struct {
	IngressClassName   string            `json:"ingressClassName"`
	Host               string            `json:"host"`
	TLS                []IngressTLS      `json:"tls,omitempty"`
	IngressAnnotations map[string]string `json:"ingressAnnotations,omitempty"`
}

// IngressTLS defines the TLS configuration for the ingress.
type IngressTLS struct {
	Hosts      []string `json:"hosts,omitempty"`
	SecretName string   `json:"secretName,omitempty"`
}

// AuthConfig defines the authentication configuration.
type AuthConfig struct {
	Kind           string `json:"kind"`
	ProviderUrl    string `json:"providerUrl"`
	RedirectUrl    string `json:"redirectUrl"`
	ClientId       string `json:"clientId"`
	Scopes         string `json:"scopes"`
	ProviderLogout bool   `json:"providerLogout,omitempty"`
}

// ScalityUIStatus defines the observed state of ScalityUI
type ScalityUIStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScalityUI is the Schema for the scalityuis API
type ScalityUI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScalityUISpec   `json:"spec,omitempty"`
	Status ScalityUIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScalityUIList contains a list of ScalityUI
type ScalityUIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScalityUI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScalityUI{}, &ScalityUIList{})
}
