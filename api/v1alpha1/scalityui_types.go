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
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ScalityUISpec defines the desired state of ScalityUI
type ScalityUISpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Image       string     `json:"image"`
	ProductName string     `json:"productName"`
	Themes      Themes     `json:"themes,omitempty"`
	Navbar      Navbar     `json:"navbar,omitempty"`
	Networks    UINetworks `json:"networks,omitempty"`
}

// Themes defines the various themes supported by the UI.
type Themes struct {
	// Light is the light theme configuration for the UI.
	Light Theme `json:"light"`
	// Dark is the dark theme configuration for the UI.
	Dark Theme `json:"dark"`
}

// Theme defines a theme supported by the UI.
type Theme struct {
	// Type specifies the theme's category or system (e.g., 'core-ui').
	Type string `json:"type"`
	// Name is the unique identifier for the theme.
	Name string `json:"name"`
	// Logo contains the logo configuration for this theme.
	Logo Logo `json:"logo"`
}

// Logo defines the logo configuration with support for different formats.
type Logo struct {
	// Type specifies the logo type: "path", "base64", or "svg".
	Type string `json:"type"`
	// Value contains the logo data based on the type:
	// - For "path": file path to the logo image
	// - For "base64": base64-encoded image data
	// - For "svg": inline SVG content
	Value string `json:"value"`
	// MimeType specifies the MIME type when using base64 (e.g., "image/png", "image/jpeg").
	// Optional for "path" and "svg" types.
	MimeType string `json:"mimeType,omitempty"`
}

// Navbar configures the UI navbar.
type Navbar struct {
	// Main contains configuration for the main navigation items in the navbar.
	Main []NavbarItem `json:"main,omitempty"`
	// SubLogin contains configuration for navigation items displayed in the sub-login area.
	SubLogin []NavbarItem `json:"subLogin,omitempty"`
}

// NavbarItem defines an item in the navbar that can be either internal or external.
type NavbarItem struct {
	// Internal contains configuration for internal navigation items.
	// Only one of Internal or External should be specified.
	Internal *InternalNavbarItem `json:"internal,omitempty"`
	// External contains configuration for external navigation items.
	// Only one of Internal or External should be specified.
	External *ExternalNavbarItem `json:"external,omitempty"`
}

// InternalNavbarItem defines an internal navigation item that links to views within the application.
type InternalNavbarItem struct {
	// Kind specifies the type of navbar item.
	Kind string `json:"kind"`
	// View identifies the associated view for this navbar item.
	View string `json:"view"`
	// Groups contains a list of user groups that can see this navbar item.
	// If empty, the item is visible to all users.
	Groups []string `json:"groups,omitempty"`
	// Icon is the name or path of the icon to display for this navbar item.
	Icon string `json:"icon,omitempty"`
	// Label contains localized text for the navbar item, keyed by language code.
	Label map[string]string `json:"label,omitempty"`
}

// ExternalNavbarItem defines an external navigation item that links to external resources.
type ExternalNavbarItem struct {
	// URL specifies the link destination for the external resource.
	URL string `json:"url"`
	// Groups contains a list of user groups that can see this navbar item.
	// If empty, the item is visible to all users.
	Groups []string `json:"groups,omitempty"`
	// Icon is the name or path of the icon to display for this navbar item.
	Icon string `json:"icon,omitempty"`
	// Label contains localized text for the navbar item, keyed by language code.
	Label map[string]string `json:"label,omitempty"`
}

// UINetworks configures network parameters for the UI.
type UINetworks struct {
	// IngressClassName specifies which ingress controller should implement the resource.
	IngressClassName string `json:"ingressClassName"`
	// Host specifies the hostname for the UI ingress.
	Host string `json:"host"`
	// TLS configures the TLS settings for the ingress.
	TLS []networkingv1.IngressTLS `json:"tls,omitempty"`
	// IngressAnnotations provides custom annotations for the ingress resource.
	IngressAnnotations map[string]string `json:"ingressAnnotations,omitempty"`
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
