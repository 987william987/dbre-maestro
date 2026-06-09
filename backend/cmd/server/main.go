package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dbre-maestro/maestro/internal/config"
	"github.com/dbre-maestro/maestro/internal/db"
	"github.com/dbre-maestro/maestro/internal/handler"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "run migrations and exit")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	migrationsPath := filepath.Join("migrations")
	if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
		// When running from project root during development
		migrationsPath = filepath.Join("backend", "migrations")
	}

	slog.Info("running migrations")
	if err := db.RunMigrations(cfg.MigrationDSN, migrationsPath); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations complete")

	if *migrateOnly {
		return
	}

	metaDB, err := db.Open(cfg.DBDSN)
	if err != nil {
		slog.Error("db open failed", "err", err)
		os.Exit(1)
	}
	defer metaDB.Close()

	if err := db.Ping(metaDB); err != nil {
		slog.Error("db ping failed", "err", err)
		os.Exit(1)
	}

	// Crash recovery: mark any executing tickets as interrupted
	ticketRepo := repository.NewTicketRepo(metaDB)
	n, err := ticketRepo.MarkInterruptedAll(context.Background())
	if err != nil {
		slog.Warn("crash recovery scan failed", "err", err)
	} else if n > 0 {
		slog.Warn("crash recovery: marked tickets as interrupted", "count", n)
	}

	userRepo := repository.NewUserRepo(metaDB)
	sessionRepo := repository.NewSessionRepo(metaDB)
	auditRepo := repository.NewAuditRepo(metaDB)
	dbConnRepo := repository.NewDBConnectionRepo(metaDB, cfg.EncryptionKey)
	exportRepo := repository.NewExportRepo(metaDB)

	var larkClient *notification.Client
	if cfg.LarkWebhookURL != "" {
		larkClient = notification.NewClient(notification.Config{
			Mode:       notification.ModeWebhook,
			WebhookURL: cfg.LarkWebhookURL,
		})
		slog.Info("lark notifications enabled")
	}

	healthH := handler.NewHealthHandler(metaDB)
	authH := handler.NewAuthHandler(userRepo, sessionRepo, auditRepo, cfg.JWTSecret)
	ticketH := handler.NewTicketHandler(ticketRepo, auditRepo, larkClient)
	dbConnH := handler.NewDBConnectionHandler(dbConnRepo, auditRepo)
	exportH := handler.NewExportHandler(exportRepo, dbConnRepo, auditRepo)
	auditH := handler.NewAuditHandler(auditRepo)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/health", healthH.ServeHTTP)

	r.Post("/setup", authH.Setup)
	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", authH.Login)
		r.Post("/refresh", authH.Refresh)
		r.With(
			middleware.RequireAuth(cfg.JWTSecret),
			middleware.InjectAuthGroups(userRepo),
		).Get("/me", authH.Me)
		r.With(middleware.RequireAuth(cfg.JWTSecret)).Post("/logout", authH.Logout)
	})

	r.Route("/exports", func(r chi.Router) {
		r.With(middleware.RequireAuth(cfg.JWTSecret)).Post("/", exportH.Create)
		// Download is token-authenticated — no JWT required
		r.Get("/download/{token}", exportH.Download)
	})

	r.Route("/db-connections", func(r chi.Router) {
		r.Use(middleware.RequireAuth(cfg.JWTSecret))
		r.Use(middleware.InjectAuthGroups(userRepo))
		r.Use(requireDBAOrAbove)

		r.Get("/", dbConnH.List)
		r.Post("/", dbConnH.Create)
		r.Post("/{id}/test", dbConnH.Test)
		r.Delete("/{id}", dbConnH.Delete)
	})

	r.Route("/audit-logs", func(r chi.Router) {
		r.Use(middleware.RequireAuth(cfg.JWTSecret))
		r.Use(middleware.InjectAuthGroups(userRepo))
		r.Use(requireDBAOrAbove)
		r.Get("/", auditH.List)
	})

	r.Route("/tickets", func(r chi.Router) {
		r.Use(middleware.RequireAuth(cfg.JWTSecret))
		r.Use(middleware.InjectAuthGroups(userRepo))

		r.Get("/", ticketH.List)
		r.Post("/", ticketH.Create)

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", ticketH.Get)
			r.With(requireReviewerOrAbove).Post("/approve", ticketH.Approve)
			r.With(requireReviewerOrAbove).Post("/reject", ticketH.Reject)
			r.With(requireDBAOrAbove).Post("/request-execution", ticketH.RequestExecution)
			r.With(requireDBAOrAbove).Post("/execute", ticketH.Execute)
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func requireReviewerOrAbove(next http.Handler) http.Handler {
	return middleware.RequireGroup(
		model.AuthGroupReviewer,
		model.AuthGroupDBA,
		model.AuthGroupAdmin,
	)(next)
}

func requireDBAOrAbove(next http.Handler) http.Handler {
	return middleware.RequireGroup(
		model.AuthGroupDBA,
		model.AuthGroupAdmin,
	)(next)
}
