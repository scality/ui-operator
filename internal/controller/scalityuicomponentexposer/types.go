package scalityuicomponentexposer

// MicroAppRuntimeConfiguration represents the runtime configuration structure
type MicroAppRuntimeConfiguration struct {
	Kind       string                               `json:"kind"`
	APIVersion string                               `json:"apiVersion"`
	Metadata   MicroAppRuntimeConfigurationMetadata `json:"metadata"`
	Spec       MicroAppRuntimeConfigurationSpec     `json:"spec"`
}

// MicroAppRuntimeConfigurationMetadata represents the metadata section
type MicroAppRuntimeConfigurationMetadata struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// MicroAppRuntimeConfigurationSpec represents the spec section
type MicroAppRuntimeConfigurationSpec struct {
	AppHistoryBasePath string                 `json:"appHistoryBasePath,omitempty"`
	Auth               map[string]interface{} `json:"auth,omitempty"`
	SelfConfiguration  map[string]interface{} `json:"selfConfiguration,omitempty"`
}
