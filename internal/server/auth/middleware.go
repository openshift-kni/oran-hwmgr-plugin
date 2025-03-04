package auth

import (
	"fmt"

	"k8s.io/client-go/rest"

	"github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api"
)

// GetAuthenticator builds authentication middleware to be used to extract user/group identity from incoming requests
func GetAuthenticator() (api.Middleware, error) {
	// Setup kubernetes config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	authenticatorConfig := KubernetesAuthenticatorConfig{
		RESTConfig: restConfig,
	}
	k8sAuthenticator, err := authenticatorConfig.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s authenticator: %w", err)
	}

	return Authenticator(k8sAuthenticator), nil
}

// GetAuthorizer builds authorization middleware to be used authorize incoming requests
func GetAuthorizer() (api.Middleware, error) {
	// Setup kubernetes config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	// Setup authorizer
	c := KubernetesAuthorizerConfig{RESTConfig: restConfig}
	k8sAuthorizer, err := c.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s authorizer: %w", err)
	}

	return Authorizer(k8sAuthorizer), nil
}
