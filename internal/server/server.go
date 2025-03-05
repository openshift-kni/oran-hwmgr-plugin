package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/server/auth"
)

// Server config values
const (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
	idleTimeout  = 120 * time.Second
)

// RunServer starts the API server and blocks until it terminates or context is canceled.
func RunServer(ctx context.Context, address, tlsCertDir string, hwMgrAdaptor *adaptors.HwMgrAdaptorController) error {
	slog.InfoContext(ctx, "Starting inventory API server")
	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sig := <-shutdown
		slog.InfoContext(ctx, "Shutdown signal received", slog.String("signal", sig.String()))
		cancel()
	}()

	// Init server
	// Create the handler
	server := api.InventoryServer{
		HwMgrAdaptor: hwMgrAdaptor,
	}

	serverStrictHandler := generated.NewStrictHandlerWithOptions(&server, nil,
		generated.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  api.GetRequestErrorFunc(),
			ResponseErrorHandlerFunc: api.GetResponseErrorFunc(),
		},
	)

	router := http.NewServeMux()
	// Register a default handler that replies with 404 so that we can override the response format
	router.HandleFunc("/", api.GetNotFoundFunc())

	// This also validates the spec file
	swagger, err := generated.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get swagger: %w", err)
	}

	// Create authn/authz middleware
	authn, err := auth.GetAuthenticator()
	if err != nil {
		return fmt.Errorf("error setting up authenticator middleware: %w", err)
	}

	authz, err := auth.GetAuthorizer()
	if err != nil {
		return fmt.Errorf("error setting up authorizer middleware: %w", err)
	}

	opt := generated.StdHTTPServerOptions{
		BaseRouter: router,
		Middlewares: []generated.MiddlewareFunc{ // Add middlewares here
			api.GetOpenAPIValidationFunc(swagger),
			authz,
			authn,
			api.GetLogDurationFunc(),
		},
		ErrorHandlerFunc: api.GetRequestErrorFunc(),
	}

	// Register the handler
	generated.HandlerWithOptions(serverStrictHandler, opt)

	certFile := filepath.Join(tlsCertDir, "tls.crt")
	keyFile := filepath.Join(tlsCertDir, "tls.key")
	serverTLSConfig, err := utils.GetServerTLSConfig(ctx, certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to get server TLS config: %w", err)
	}

	// Server config
	srv := &http.Server{
		Handler:      router,
		Addr:         address,
		TLSConfig:    serverTLSConfig,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		ErrorLog: slog.NewLogLogger(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
		}), slog.LevelError),
	}

	// Start server
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info(fmt.Sprintf("Inventory API server Listening on %s", srv.Addr))
		// Cert/Key files aren't needed here since they've been added to the tls.Config above.
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	defer func() {
		// Cancel the context in case it wasn't already canceled
		cancel()
		// Shutdown the http server
		slog.InfoContext(ctx, "Shutting down inventory API server")
		if err := api.GracefulShutdown(srv); err != nil {
			slog.Error("error shutting down inventory API server", "error", err)
		}
	}()

	// Blocking select
	select {
	case err := <-serverErrors:
		return fmt.Errorf("error starting inventory API server: %w", err)
	case <-ctx.Done():
		slog.InfoContext(ctx, "Inventory API server shutting down")
	}

	return nil
}
