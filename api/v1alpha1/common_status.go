package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CommonStatus defines the common status fields that can be shared across different CRs
type CommonStatus struct {
	// Phase represents the current phase of the resource deployment
	// +kubebuilder:validation:Enum=Pending;Progressing;Ready;Failed
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the resource's current state
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// Common phase constants
const (
	PhasePending     = "Pending"
	PhaseProgressing = "Progressing"
	PhaseReady       = "Ready"
	PhaseFailed      = "Failed"
)

// Common condition types
const (
	ConditionTypeReady       = "Ready"
	ConditionTypeProgressing = "Progressing"
)

// Common condition reasons
const (
	ReasonReady              = "Ready"
	ReasonNotReady           = "NotReady"
	ReasonProgressing        = "Progressing"
	ReasonReconciling        = "Reconciling"
	ReasonReconcileError     = "ReconcileError"
	ReasonConfigurationError = "ConfigurationError"
	ReasonResourceError      = "ResourceError"
)
