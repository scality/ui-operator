package utils

import (
	"fmt"

	uiv1alpha1 "github.com/scality/ui-operator/api/v1alpha1"
)

// ValidateAuthConfig validates that all required auth fields are present
func ValidateAuthConfig(auth *uiv1alpha1.AuthConfig) error {
	requiredFields := map[string]string{
		"kind":         auth.Kind,
		"providerUrl":  auth.ProviderURL,
		"redirectUrl":  auth.RedirectURL,
		"clientId":     auth.ClientID,
		"responseType": auth.ResponseType,
		"scopes":       auth.Scopes,
	}

	for field, value := range requiredFields {
		if value == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	return nil
}

// AuthConfigToMap converts AuthConfig to map[string]interface{}
func AuthConfigToMap(auth *uiv1alpha1.AuthConfig) map[string]interface{} {
	authConfig := map[string]interface{}{
		"kind":         auth.Kind,
		"providerUrl":  auth.ProviderURL,
		"redirectUrl":  auth.RedirectURL,
		"clientId":     auth.ClientID,
		"responseType": auth.ResponseType,
		"scopes":       auth.Scopes,
	}

	if auth.ProviderLogout != nil {
		authConfig["providerLogout"] = *auth.ProviderLogout
	}

	return authConfig
}

// BuildAuthConfig builds the authentication configuration
func BuildAuthConfig(authSpec *uiv1alpha1.AuthConfig) (map[string]interface{}, error) {
	// If exposer has auth config, validate it's complete
	if authSpec != nil {
		if err := ValidateAuthConfig(authSpec); err != nil {
			return nil, fmt.Errorf("exposer auth configuration incomplete: %w", err)
		}
		return AuthConfigToMap(authSpec), nil
	}

	// No auth configuration available - ScalityUI auth will be implemented in another branch
	return map[string]interface{}{}, nil
}
