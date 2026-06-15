package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	yttv "github.com/ygelfand/lib-yttv"
	"github.com/ygelfand/yttv-bridge/internal/api"
	"github.com/ygelfand/yttv-bridge/internal/config"
	"github.com/ygelfand/yttv-bridge/internal/state"
)

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "yttv-bridge",
		Short:         "Local HTTP daemon exposing YouTube TV cast control",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(serveCmd())
	return root
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP server",
		RunE:  runServe,
	}
}

func runServe(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogger(cfg.LogLevel, cfg.LogFormat)

	session := yttv.New(&cfg.Creds)

	bootCtx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	if err := cfg.Creds.Validate(); err != nil {
		cancel()
		return err
	}
	if cfg.Creds.GoogleAccountID == "" {
		gid, err := cfg.Creds.DiscoverGoogleAccountID(bootCtx, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("discover google_account_id: %w", err)
		}
		cfg.Creds.GoogleAccountID = gid
		slog.Info("discovered google_account_id", "id", gid)
	}
	cancel()

	store := state.New(session)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store.Run(ctx, cfg.EPGRefresh, cfg.DiscoverRefresh, cfg.DiscoverTimeout)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.Router(store),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, sc := context.WithTimeout(context.Background(), 10*time.Second)
		defer sc()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func setupLogger(level slog.Level, format string) {
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}
