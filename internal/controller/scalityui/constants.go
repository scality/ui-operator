package scalityui

const (
	uiServicePort = 80
	// Deployed UI apps constants
	deployedUIAppsKey = "deployed-ui-apps.json"

	// Annotation key recording the SHA256 of the rendered
	// deployed-ui-apps.json so deployments can detect content drift.
	deployedAppsHashAnnotation = "scality.com/deployed-apps-hash"

	// Subresource hash keys for in-memory state
	configMapHashKey             = "configmap"
	deployedAppsConfigMapHashKey = "deployed-apps-configmap"
)
