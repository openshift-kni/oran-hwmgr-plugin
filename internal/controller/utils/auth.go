/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
)

// OAuthClientConfig defines the parameters required to establish an HTTP Client capable of acquiring an OAuth Token
// from an OAuth capable authorization server.
type OAuthClientConfig struct {
	// Defines a PEM encoded set of CA certificates used to validate server certificates.  If not provided then the
	// default root CA bundle will be used.
	CaBundle []byte
	// Defines the OAuth client-id attribute to be used when acquiring a token.  If not provided (for debug/testing)
	// then a normal HTTP client without OAuth capabilities will be created
	ClientId     string
	ClientSecret string
	// The absolute URL of the API endpoint to be used to acquire a token
	// (e.g., http://example.com/realms/oran/protocol/openid-connect/token)
	TokenUrl string
	// The list of OAuth scopes requested by the client.  These will be dictated by what the SMO is expecting to see in
	// the token.
	Scopes []string
	// The grant type.
	GrantType pluginv1alpha1.OAuthGrantType
	// Username, for Password grant type
	Username string
	// Password, for Password grant type
	Password string
}

// Default values for backend URL and token:
const (
	defaultBackendURL       = "https://kubernetes.default.svc"
	defaultBackendTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"          // nolint: gosec // hardcoded path only
	defaultBackendCABundle  = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"         // nolint: gosec // hardcoded path only
	defaultServiceCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt" // nolint: gosec // hardcoded path only
)

// The following regex pattern is used to match keys to automatically redact from the message tracing logs
var redactionPattern = regexp.MustCompile(`(?i)password|token|client_id|username`)

// Replacement string for redacted fields in message tracing logs
const redactedValue = "*redacted*"

// loadDefaultCABundles loads the default service account and ingress CA bundles.  This should only be invoked if TLS
// verification has not been disabled since the expectation is that it will only need to be disabled when testing as a
// standalone binary in which case the paths to the bundles won't be present.  Otherwise, we always expect the bundles
// to be present when running in-cluster.
func loadDefaultCABundles(config *tls.Config) error {
	config.RootCAs = x509.NewCertPool()
	if data, err := os.ReadFile(defaultBackendCABundle); err != nil {
		// This should not happen unless the binary is being tested in standalone mode in which case the developer
		// should have disabled the TLS verification which would prevent this function from being invoked.
		return fmt.Errorf("failed to read CA bundle '%s': %w", defaultBackendCABundle, err)
		// This should not happen, but if it does continue anyway
	} else {
		// This will enable accessing public facing API endpoints signed by the default ingress controller certificate
		config.RootCAs.AppendCertsFromPEM(data)
	}

	if data, err := os.ReadFile(defaultServiceCAFile); err != nil {
		return fmt.Errorf("failed to read service CA file '%s': %w", defaultServiceCAFile, err)
	} else {
		// This will enable accessing internal services signed by the service account signer.
		config.RootCAs.AppendCertsFromPEM(data)
	}

	return nil
}

// GetDefaultTLSConfig sets the TLS configuration attributes appropriately to enable communication between internal
// services and accessing the public facing API endpoints.
func GetDefaultTLSConfig(config *tls.Config, insecureSkipTLSVerify bool) (*tls.Config, error) {
	if config == nil {
		config = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	// Allow developers to override the TLS verification
	config.InsecureSkipVerify = insecureSkipTLSVerify
	if !config.InsecureSkipVerify {
		// TLS verification is enabled therefore we need to load the CA bundles that are injected into our filesystem
		// automatically; which happens since we are defined as using a service-account
		err := loadDefaultCABundles(config)
		if err != nil {
			return nil, fmt.Errorf("error loading default CABundles: %w", err)
		}
	}

	return config, nil
}

// GetServerTLSConfig creates a tls.Config that uses a dynamic loader to handle updates to the certificate and/or key.
func GetServerTLSConfig(ctx context.Context, certFile, keyFile string) (*tls.Config, error) {
	loader, err := dynamiccertificates.NewDynamicServingContentFromFiles("tls-server", certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to setup certificate loader: %w", err)
	}
	go loader.Run(ctx, 1)

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			certBytes, keyBytes := loader.CurrentCertKeyContent()
			cert, err := tls.X509KeyPair(certBytes, keyBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to create server certificate: %w", err)
			}
			return &cert, nil
		},
	}

	return tlsConfig, nil
}

// GetDefaultBackendTransport returns an HTTP transport with the proper TLS defaults set.
func GetDefaultBackendTransport(insecureSkipTLSVerify bool) (http.RoundTripper, error) {
	tlsConfig, err := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}, insecureSkipTLSVerify)
	if err != nil {
		return nil, err
	}

	return net.SetTransportDefaults(&http.Transport{TLSClientConfig: tlsConfig}), nil
}

func GetTransportWithCaBundle(config OAuthClientConfig, insecureSkipTLSVerify, logMessages bool) (http.RoundTripper, error) {
	tlsConfig, err := GetDefaultTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}, insecureSkipTLSVerify)
	if err != nil {
		return nil, err
	}

	if len(config.CaBundle) != 0 {
		// If the user has provided a CA bundle then we must use it to build our client so that we can verify the
		// identity of remote servers.
		if tlsConfig.RootCAs == nil {
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(config.CaBundle) {
				return nil, fmt.Errorf("failed to append certificate bundle to pool")
			}
			tlsConfig.RootCAs = certPool
		} else {
			// We may not need the default CA bundles in this case but there's no harm in keeping them in the pool
			// to handle cases where they may be needed.
			tlsConfig.RootCAs.AppendCertsFromPEM(config.CaBundle)
		}
	}

	if logMessages {
		return LoggingRoundTripper{TLSClientConfig: tlsConfig}, nil
	}

	return net.SetTransportDefaults(&http.Transport{TLSClientConfig: tlsConfig}), nil
}

// TODO: Determine whether to remove the message tracing altogether.
// Currently this writes debug logs, but the level is hardcoded. Seeing these debug logs requires
// setting the loglevel of the utilsLog logger, so this needs some work here.
type LoggingRoundTripper struct {
	TLSClientConfig *tls.Config
}

func redactObject(object interface{}) interface{} {
	switch t := object.(type) {
	case map[string]interface{}:
		for k := range t {
			if redactionPattern.MatchString(k) {
				t[k] = redactedValue
			}
		}
		return t
	case []interface{}:
		for i, v := range t {
			t[i] = redactObject(v)
		}
		return t
	}

	return object
}

func redact(msg []byte) string {
	var object interface{}
	if err := json.Unmarshal(msg, &object); err != nil {
		utilsLog.Debug("failed to unmarshal message", slog.String("error", err.Error()))
		return ""
	}

	redacted := redactObject(object)
	redactedMsg, err := json.Marshal(redacted)
	if err != nil {
		utilsLog.Debug("failed to marshal redacted message", slog.String("error", err.Error()))
	}

	return string(redactedMsg)
}

func (t LoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqStr string
	var respStr string

	if req.Body != nil {
		reqbuf, e := io.ReadAll(req.Body)
		if e != nil {
			utilsLog.Debug("Reading http request from RoundTrip injector error", slog.String("error", e.Error()))
		} else {
			reqrdr1 := io.NopCloser(bytes.NewBuffer(reqbuf))
			reqrdr2 := io.NopCloser(bytes.NewBuffer(reqbuf))
			req.Body = reqrdr2
			// read resp.Body to string
			breq, errreq := io.ReadAll(reqrdr1)
			if errreq != nil {
				utilsLog.Debug("Reading http request from RoundTrip injector error", slog.String("error", errreq.Error()))
			} else {
				reqStr = redact(breq)
			}
		}
	}

	// Do work before the request is sent
	rt := http.Transport{
		TLSClientConfig: t.TLSClientConfig}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		return resp, err // nolint: wrapcheck
	}

	if resp.Body != nil {
		respbuf, e := io.ReadAll(resp.Body)
		if e != nil {
			utilsLog.Debug("Reading http response from RoundTrip injector error", slog.String("error", e.Error()))
		} else {
			resprdr2 := io.NopCloser(bytes.NewBuffer(respbuf))
			resp.Body = resprdr2
			resprdr1 := io.NopCloser(bytes.NewBuffer(respbuf))
			// read resp.Body to string
			b, errresp := io.ReadAll(resprdr1)
			if errresp != nil {
				utilsLog.Debug("Reading http response from RoundTrip injector error", slog.String("error", errresp.Error()))
			} else {
				respStr = redact(b)
			}
		}
	}

	redactedReqHeader := req.Header
	if _, exists := redactedReqHeader["Authorization"]; exists {
		redactedReqHeader["Authorization"] = []string{redactedValue}
	}

	redactedRespHeader := resp.Header
	if _, exists := redactedRespHeader["Authorization"]; exists {
		redactedRespHeader["Authorization"] = []string{redactedValue}
	}

	// Do work after the response is received
	utilsLog.Debug(fmt.Sprintf("REQUEST(%s) %s, Headers: %+v, Body: %s, RESPONSE(%d), Headers: %+v, Body: %s",
		req.Method,
		req.URL.Path,
		req.Header,
		reqStr,
		resp.StatusCode,
		resp.Header,
		respStr))

	return resp, err // nolint: wrapcheck
}

// SetupOAuthClient creates an HTTP client capable of acquiring an OAuth token used to authorize client requests.  If
// the config excludes the OAuth specific sections then the client produced is a simple HTTP client without OAuth
// capabilities.
func SetupOAuthClient(ctx context.Context, config OAuthClientConfig, insecureSkipTLSVerify, logMessages bool) (*http.Client, error) {
	tr, err := GetTransportWithCaBundle(config, insecureSkipTLSVerify, logMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to get http transport: %w", err)
	}

	c := &http.Client{
		Transport: tr,
	}

	if config.ClientId != "" {
		var clientConfig clientcredentials.Config
		switch config.GrantType {
		case pluginv1alpha1.OAuthGrantTypes.ClientCredentials:
			clientConfig = clientcredentials.Config{
				ClientID:       config.ClientId,
				ClientSecret:   config.ClientSecret,
				TokenURL:       config.TokenUrl,
				Scopes:         config.Scopes,
				EndpointParams: nil,
				AuthStyle:      oauth2.AuthStyleInParams,
			}

		case pluginv1alpha1.OAuthGrantTypes.Password:
			clientConfig = clientcredentials.Config{
				ClientID: config.ClientId,
				TokenURL: config.TokenUrl,
				Scopes:   config.Scopes,
				EndpointParams: url.Values{
					"grant_type": {string(config.GrantType)},
					"username":   {config.Username},
					"password":   {config.Password},
				},
				AuthStyle: oauth2.AuthStyleAutoDetect,
			}
		default:
			return nil, fmt.Errorf("unsupported grant_type: %s", config.GrantType)
		}

		ctx = context.WithValue(ctx, oauth2.HTTPClient, c)

		c = clientConfig.Client(ctx)
	}

	return c, nil
}
