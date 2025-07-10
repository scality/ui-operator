package scalityuicomponentexposer

import "time"

const (
	// ConfigMap related constants
	configMapNameSuffix = "runtime-app-configuration"

	// Condition types
	conditionTypeConfigMapReady    = "ConfigMapReady"
	conditionTypeDependenciesReady = "DependenciesReady"
	conditionTypeDeploymentReady   = "DeploymentReady"
	conditionTypeIngressReady      = "IngressReady"

	// Condition reasons
	reasonReconcileSucceeded = "ReconcileSucceeded"
	reasonReconcileFailed    = "ReconcileFailed"
	reasonDependencyMissing  = "DependencyMissing"

	// Requeue intervals
	defaultRequeueInterval = time.Minute

	// Runtime configuration constants
	runtimeConfigKind       = "MicroAppRuntimeConfiguration"
	runtimeConfigAPIVersion = "ui.scality.com/v1alpha1"

	// Mount and deployment related constants
	configHashAnnotation = "ui.scality.com/config-hash"
	volumeNamePrefix     = "config-volume-"

	// Finalizers
	exposerFinalizer         = "uicomponentexposer.scality.com/finalizer"
	configMapFinalizerPrefix = "uicomponentexposer.scality.com/"

	// Mount path constants
	configsSubdirectory = "configs"
)
