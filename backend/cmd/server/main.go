package main

import (
	"context"
	"database/sql"
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
	"github.com/dbre-maestro/maestro/internal/job"
	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/realtime"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
)

const (
	requestTimeout = 45 * time.Second
	writeTimeout   = 45 * time.Second
)

func timeoutExceptEventStream(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/events/stream" {
				next.ServeHTTP(w, r)
				return
			}
			chimw.Timeout(timeout)(next).ServeHTTP(w, r)
		})
	}
}

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

	pool.SetProfileConfigs(cfg.PoolProfiles)

	shadowValidationRawDB, err := pool.Open("mysql", cfg.DBDSN, pool.ProfileShadowValidation)
	if err != nil {
		slog.Error("shadow validation db open failed", "err", err)
		os.Exit(1)
	}
	defer shadowValidationRawDB.Close()
	shadowValidationDB := dbxFromStdlib(shadowValidationRawDB)

	// Crash recovery: mark any executing tickets as interrupted
	ticketRepo := repository.NewTicketRepo(metaDB)
	queryAccessRepo := repository.NewQueryAccessRepo(metaDB)
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
	queryArtifactRepo := repository.NewQueryArtifactRepo(metaDB)
	notifRepo := repository.NewNotificationRepo(metaDB)
	whitelistRepo := repository.NewMaskingWhitelistRepo(metaDB)
	authGroupRepo := repository.NewAuthGroupRepo(metaDB)
	settingsRepo := repository.NewSettingsRepo(metaDB, cfg.EncryptionKey)
	dbMetadataRepo := repository.NewDBMetadataRepo(metaDB)
	scheduledReportRepo := repository.NewScheduledSQLReportRepo(metaDB)

	larkDispatcher := notification.NewDispatcher(settingsRepo, userRepo, cfg.LarkWebhookURL)
	if cfg.LarkWebhookURL != "" {
		slog.Info("lark webhook fallback enabled")
	}

	maskingRuleRepo := repository.NewMaskingRuleRepo(metaDB)
	sqlReviewRuleRepo := repository.NewSQLReviewRuleRepo(metaDB)
	eventBroker := realtime.NewBroker()

	maskingEngine, err := masking.NewEngine(cfg.EncryptionKey, masking.GlobalCache())
	if err != nil {
		slog.Error("masking engine init failed", "err", err)
		os.Exit(1)
	}

	healthH := handler.NewHealthHandler(metaDB)
	authH := handler.NewAuthHandler(userRepo, sessionRepo, auditRepo, cfg.JWTSecret, cfg.RefreshCookieSecure)
	ticketH := handler.NewTicketHandler(ticketRepo, queryAccessRepo, exportRepo, auditRepo, settingsRepo, dbConnRepo, userRepo, maskingRuleRepo, whitelistRepo, maskingEngine, sqlReviewRuleRepo, shadowValidationDB, larkDispatcher, notifRepo, eventBroker, cfg.AppBaseURL)
	dbConnH := handler.NewDBConnectionHandler(dbConnRepo, userRepo, authGroupRepo, auditRepo)
	exportH := handler.NewExportHandler(exportRepo, ticketRepo, dbConnRepo, userRepo, auditRepo, settingsRepo, queryAccessRepo, maskingRuleRepo, whitelistRepo, maskingEngine, notifRepo, eventBroker, larkDispatcher, cfg.AppBaseURL)
	auditH := handler.NewAuditHandler(auditRepo)
	maskingRuleH := handler.NewMaskingRuleHandler(maskingRuleRepo, auditRepo, masking.GlobalCache())
	sqlReviewRuleH := handler.NewSQLReviewRuleHandler(sqlReviewRuleRepo, auditRepo)
	queryH := handler.NewQueryHandler(dbConnRepo, userRepo, maskingRuleRepo, auditRepo, queryArtifactRepo, ticketRepo, settingsRepo, queryAccessRepo, maskingEngine, whitelistRepo, notifRepo, eventBroker, larkDispatcher, cfg.AppBaseURL)
	userH := handler.NewUserHandler(userRepo, authGroupRepo, sessionRepo, auditRepo, dbConnRepo)
	queryAccessAdminH := handler.NewQueryAccessAdminHandler(queryAccessRepo, userRepo, authGroupRepo, auditRepo)
	metadataH := handler.NewMetadataHandler(dbConnRepo, userRepo)
	authGroupH := handler.NewAuthGroupHandler(authGroupRepo, userRepo, auditRepo)
	notifH := handler.NewNotificationHandler(notifRepo)
	eventStreamH := handler.NewEventStreamHandler(eventBroker)
	whitelistH := handler.NewMaskingWhitelistHandler(dbConnRepo, whitelistRepo, auditRepo)
	settingsH := handler.NewSettingsHandler(settingsRepo, userRepo, authGroupRepo, dbConnRepo, auditRepo)
	dbMetadataH := handler.NewDBMetadataHandler(dbMetadataRepo, dbConnRepo, settingsRepo)
	scheduledReportH := handler.NewScheduledSQLReportHandler(scheduledReportRepo, dbConnRepo, userRepo, queryAccessRepo, maskingRuleRepo, whitelistRepo, ticketRepo, maskingEngine, auditRepo, larkDispatcher)
	inventoryJob := job.NewDBMetadataInventoryJob(settingsRepo, dbMetadataRepo, logger)
	objectJob := job.NewDBMetadataObjectJob(settingsRepo, dbConnRepo, dbMetadataRepo, logger)

	// Background scheduler: poll every 30s for due scheduled tickets
	go runScheduler(ticketRepo, dbConnRepo, ticketH)
	go runScheduledSQLReportScheduler(scheduledReportH)
	go inventoryJob.Start(context.Background())
	go objectJob.Start(context.Background())

	r := chi.NewRouter()
	r.Use(middleware.SecurityHeaders(cfg.AppEnv == "production"))
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(timeoutExceptEventStream(requestTimeout))

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", healthH.ServeHTTP)

		r.Get("/setup/status", authH.SetupStatus)
		r.Post("/setup", authH.Setup)
		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", authH.Login)
			r.Post("/refresh", authH.Refresh)
			r.With(
				middleware.RequireAuth(cfg.JWTSecret),
				middleware.RequireActiveUser(userRepo),
				middleware.InjectPermissions(userRepo),
			).Get("/me", authH.Me)
			r.With(
				middleware.RequireAuth(cfg.JWTSecret),
				middleware.RequireActiveUser(userRepo),
			).Post("/logout", authH.Logout)
		})

		r.Route("/exports", func(r chi.Router) {
			r.With(
				middleware.RequireAuth(cfg.JWTSecret),
				middleware.RequireActiveUser(userRepo),
				middleware.InjectPermissions(userRepo),
				middleware.RequirePermission("sql_editor.export"),
			).Post("/", exportH.Create)
			r.Get("/download/{token}", exportH.Download)
		})

		r.Route("/db-connections", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))

			r.With(requireDBConnectionsRead).Get("/", dbConnH.List)
			r.With(requireDBConnectionsRead).Get("/{id}/bindings", dbConnH.Bindings)
			r.With(requireDBConnectionsWrite).Post("/", dbConnH.Create)
			r.With(requireDBConnectionsWrite).Patch("/{id}", dbConnH.Patch)
			r.With(requireDBConnectionsWrite).Post("/{id}/test", dbConnH.Test)
			r.With(requireDBConnectionsWrite).Delete("/{id}", dbConnH.Delete)
		})

		r.Route("/audit-logs", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireAuditLogsRead).Get("/", auditH.List)
			r.With(requireAuditLogsWrite).Get("/export", auditH.Export)
		})

		r.Route("/users", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireUsersRead).Get("/", userH.List)
			r.With(requireUsersRead).Get("/db-connections", userH.ListDBConnections)
			r.With(requireUsersRead).Get("/query-access-rules", queryAccessAdminH.List)
			r.With(requireUsersWrite).Post("/query-access-rules", queryAccessAdminH.Create)
			r.With(requireUsersWrite).Post("/query-access-rules/{id}/revoke", queryAccessAdminH.Revoke)
			r.With(requireUsersWrite).Post("/", userH.Create)
			r.With(requireUsersRead).Get("/{id}", userH.Get)
			r.With(requireUsersWrite).Patch("/{id}", userH.Patch)
			r.With(requireUsersWrite).Delete("/{id}", userH.Delete)
			r.With(requireUsersWrite).Post("/{id}/memberships", userH.AddMembership)
			r.With(requireUsersWrite).Delete("/{id}/memberships/{group}", userH.RemoveMembership)
			r.With(requireUsersWrite).Post("/{id}/permissions", userH.AddDirectPermission)
			r.With(requireUsersWrite).Delete("/{id}/permissions/{permissionKey}", userH.RemoveDirectPermission)
			r.With(requireUsersWrite).Post("/{id}/db-connections", userH.AddDirectDBConnection)
			r.With(requireUsersWrite).Delete("/{id}/db-connections/{connID}", userH.RemoveDirectDBConnection)
		})

		r.Route("/settings", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireSettingsRead).Get("/", settingsH.Get)
			r.With(requireSettingsRead).Get("/db-connections", settingsH.ListDBConnections)
			r.With(requireSettingsRead).Get("/approval-resolution", settingsH.ApprovalResolution)
			r.With(requireSettingsRead).Get("/workflow-rules", settingsH.ListWorkflowRules)
			r.With(requireSettingsWrite).Put("/workflow-rules", settingsH.ReplaceWorkflowRules)
			r.With(requireSettingsRead).Post("/workflow-rules/preview", settingsH.PreviewWorkflowRule)
			r.With(requireSettingsRead).Post("/workflow-rules/effective-preview", settingsH.PreviewWorkflowRules)
			r.With(requireSettingsRead).Post("/workflow-rules/simulate", settingsH.SimulateWorkflowRule)
			r.With(requireSettingsWrite).Patch("/", settingsH.Patch)
		})

		r.Route("/db-metadata", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireDBMetadataRead).Get("/inventory", dbMetadataH.ListInventory)
			r.With(requireDBMetadataRead).Get("/objects", dbMetadataH.ListObjects)
		})

		r.Route("/auth-groups", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireUsersRead).Get("/", authGroupH.List)
			r.With(requireUsersWrite).Post("/", authGroupH.Create)
			r.With(requireUsersRead).Get("/{group}", authGroupH.Get)
			r.With(requireUsersWrite).Patch("/{group}", authGroupH.Patch)
			r.With(requireUsersWrite).Delete("/{group}", authGroupH.Delete)
			r.With(requireUsersWrite).Post("/{group}/permissions", authGroupH.AddPermission)
			r.With(requireUsersWrite).Delete("/{group}/permissions/{permissionKey}", authGroupH.RemovePermission)
			r.With(requireUsersWrite).Post("/{group}/db-connections", authGroupH.AddDBConnection)
			r.With(requireUsersWrite).Delete("/{group}/db-connections/{connID}", authGroupH.RemoveDBConnection)
		})

		r.Route("/masking-rules", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireMaskingRulesRead).Get("/", maskingRuleH.List)
			r.With(requireMaskingRulesWrite).Post("/", maskingRuleH.Create)
			r.With(requireMaskingRulesWrite).Patch("/{id}", maskingRuleH.Patch)
			r.With(requireMaskingRulesWrite).Delete("/{id}", maskingRuleH.Delete)
		})

		r.Route("/masking-whitelist", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireMaskingRulesRead).Get("/", whitelistH.List)
			r.With(requireMaskingRulesRead).Get("/connections", whitelistH.ListConnections)
			r.With(requireMaskingRulesRead).Get("/connections/{id}/metadata", whitelistH.ListMetadata)
			r.With(requireMaskingRulesRead).Get("/connections/{id}/metadata/{schema}/{table}/columns", whitelistH.ListColumns)
			r.With(requireMaskingRulesWrite).Post("/", whitelistH.Create)
			r.With(requireMaskingRulesWrite).Patch("/{id}", whitelistH.Patch)
			r.With(requireMaskingRulesWrite).Delete("/{id}", whitelistH.Delete)
		})

		r.Route("/sql-review-rules", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireSQLReviewRead).Get("/", sqlReviewRuleH.List)
			r.With(requireSQLReviewWrite).Patch("/{name}", sqlReviewRuleH.Patch)
		})

		r.Route("/query", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireSQLEditorQuery).Get("/connections", queryH.ListConnections)
			r.With(requireSQLEditorRead).Get("/constraints", queryH.Constraints)
			r.With(requireSQLEditorQuery).Post("/", queryH.Execute)
			r.With(requireSQLEditorSensitiveApply).Post("/sensitive-access", queryH.CreateSensitiveAccessTicket)
			r.With(requireSQLEditorQuery).Get("/history", queryH.ListHistory)
			r.With(requireSQLEditorQuery).Get("/saved-queries", queryH.ListSavedQueries)
			r.With(requireSQLEditorQuery).Post("/saved-queries", queryH.CreateSavedQuery)
			r.With(requireSQLEditorQuery).Delete("/saved-queries/{id}", queryH.DeleteSavedQuery)
		})

		r.Route("/scheduled-sql-reports", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireScheduledSQLReportsRead).Get("/", scheduledReportH.List)
			r.With(requireScheduledSQLReportsRead).Get("/connections", scheduledReportH.ListConnections)
			r.With(requireScheduledSQLReportsRead).Get("/recipients", scheduledReportH.ListRecipients)
			r.With(requireScheduledSQLReportsWrite).Post("/", scheduledReportH.Create)
			r.With(requireScheduledSQLReportsRead).Get("/{id}", scheduledReportH.Get)
			r.With(requireScheduledSQLReportsWrite).Patch("/{id}", scheduledReportH.Update)
			r.With(requireScheduledSQLReportsWrite).Delete("/{id}", scheduledReportH.Delete)
		})

		r.Route("/db-connections/{id}/metadata", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.With(requireSQLEditorQuery).Get("/", metadataH.Tables)
			r.With(requireSQLEditorQuery).Get("/{schema}/{table}/columns", metadataH.Columns)
			r.With(requireSQLEditorQuery).Get("/{schema}/{table}/definition", metadataH.Definition)
		})

		r.Route("/tickets", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))

			r.With(requireTicketsRead).Get("/", ticketH.List)
			r.With(requireTicketsRead).Get("/workflow-dashboard-summary", ticketH.WorkflowDashboardSummary)
			r.With(requireTicketsApply).Get("/connections", ticketH.ListConnections)
			r.With(requireTicketsApply).Get("/connections/{id}/databases", ticketH.ListDatabases)
			r.With(requireTicketsApply).Post("/review", ticketH.ReviewSQL)
			r.With(requireSettingsWrite).Post("/retry-workflow-resolution-batch", ticketH.RetryWorkflowResolutionBatch)
			r.With(requireTicketsApply).Post("/", ticketH.Create)

			r.Route("/{id}", func(r chi.Router) {
				r.With(requireTicketsRead).Get("/", ticketH.Get)
				r.With(requireTicketWorkflowReview).Post("/approve", ticketH.Approve)
				r.With(requireTicketWorkflowReject).Post("/reject", ticketH.Reject)
				r.With(requireTicketsApply).Post("/withdraw", ticketH.Withdraw)
				r.With(requireSensitiveReview).Post("/revoke", ticketH.Revoke)
				r.With(requireTicketsExecute).Post("/execute", ticketH.Execute)
				r.With(requireTicketsExecute).Post("/stop", ticketH.Stop)
				r.With(requireSettingsWrite).Post("/retry-workflow-resolution", ticketH.RetryWorkflowResolution)
			})
		})

		r.Route("/notifications", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Use(middleware.InjectPermissions(userRepo))
			r.Get("/", notifH.List)
			r.Post("/read-all", notifH.MarkAllRead)
			r.Post("/{id}/read", notifH.MarkRead)
		})

		r.Route("/events", func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Use(middleware.RequireActiveUser(userRepo))
			r.Get("/stream", eventStreamH.Stream)
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: writeTimeout,
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

func dbxFromStdlib(raw *sql.DB) *sqlx.DB {
	return sqlx.NewDb(raw, "mysql")
}

// runScheduler polls every 30 seconds for scheduled tickets whose scheduled_at has passed,
// then triggers immediate execution for each.
func runScheduler(tickets *repository.TicketRepo, dbConns *repository.DBConnectionRepo, ticketH *handler.TicketHandler) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		due, err := tickets.GetDueScheduled(context.Background())
		if err != nil {
			slog.Warn("scheduler: GetDueScheduled failed", "err", err)
			continue
		}
		for i := range due {
			t := due[i]
			// Use executor_id stored in the ticket; fall back to 0 (system)
			executorID := uint64(0)
			if t.ExecutorID != nil {
				executorID = *t.ExecutorID
			}
			ok, err := tickets.StartExecution(context.Background(), t.ID, executorID)
			if err != nil || !ok {
				continue // already taken or error
			}
			slog.Info("scheduler: starting execution", "ticket_id", t.ID, "ticket_no", t.TicketNo)
			go ticketH.RunScheduledTicket(&t, executorID)
		}
	}
}

func runScheduledSQLReportScheduler(reportH *handler.ScheduledSQLReportHandler) {
	ticker := time.NewTicker(scheduledSQLReportSchedulerPollInterval)
	defer ticker.Stop()
	for range ticker.C {
		reportH.RunDueReports(context.Background())
	}
}

const scheduledSQLReportSchedulerPollInterval = time.Minute

func requireUsersRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("users.read", "users.write")(next)
}
func requireUsersWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("users.write")(next)
}
func requireAuditLogsRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("audit_logs.read", "audit_logs.write")(next)
}
func requireAuditLogsWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("audit_logs.write")(next)
}
func requireDBConnectionsRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("db_connections.read", "db_connections.write")(next)
}
func requireDBConnectionsWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("db_connections.write")(next)
}
func requireMaskingRulesRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("masking_rules.read", "masking_rules.write")(next)
}
func requireMaskingRulesWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("masking_rules.write")(next)
}
func requireSQLReviewRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_review.read", "sql_review.write")(next)
}
func requireSQLReviewWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_review.write")(next)
}
func requireSQLEditorQuery(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_editor.query")(next)
}
func requireSQLEditorRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_editor.read")(next)
}
func requireScheduledSQLReportsRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("scheduled_sql_reports.read", "scheduled_sql_reports.write")(next)
}
func requireScheduledSQLReportsWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("scheduled_sql_reports.write")(next)
}
func requireSQLEditorSensitiveApply(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_editor.sensitive_apply")(next)
}
func requireSensitiveReview(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_editor.sensitive_review")(next)
}
func requireSQLEditorExportReview(next http.Handler) http.Handler {
	return middleware.RequirePermission("sql_editor.export_review")(next)
}
func requireTicketsApply(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.apply")(next)
}
func requireTicketsRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.read")(next)
}
func requireTicketsReview(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.review")(next)
}
func requireTicketsExecute(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.execute")(next)
}
func requireTicketsWorkspaceRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.read")(next)
}
func requireTicketWorkflowReview(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.review", "sql_editor.export_review", "sql_editor.sensitive_review")(next)
}
func requireTicketWorkflowReject(next http.Handler) http.Handler {
	return middleware.RequirePermission("tickets.review", "tickets.execute", "sql_editor.export_review", "sql_editor.sensitive_review")(next)
}
func requireSettingsRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("settings.read", "settings.write")(next)
}
func requireSettingsWrite(next http.Handler) http.Handler {
	return middleware.RequirePermission("settings.write")(next)
}
func requireDBMetadataRead(next http.Handler) http.Handler {
	return middleware.RequirePermission("db_metadata.read")(next)
}
