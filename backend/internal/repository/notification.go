package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type NotificationRepo struct {
	db *sqlx.DB
}

type NotificationDelivery struct {
	NotificationType string
	ResourceType     string
	ResourceID       uint64
	UserID           uint64
	Channel          string
	Status           string
	Attempts         int
	ErrorMessage     string
}

type NotificationHealthStats struct {
	LarkFailed7d                int64                    `db:"lark_failed_7d" json:"lark_failed_7d"`
	InteractiveCallbackFailed7d int64                    `db:"interactive_callback_failed_7d" json:"interactive_callback_failed_7d"`
	RetryOrFailure7d            int64                    `db:"retry_or_failure_7d" json:"retry_or_failure_7d"`
	MissingLarkRecipient7d      int64                    `db:"missing_lark_recipient_7d" json:"missing_lark_recipient_7d"`
	RecipientConflict7d         int64                    `db:"recipient_conflict_7d" json:"recipient_conflict_7d"`
	ByType                      []WorkflowDashboardCount `json:"by_type"`
}

func NewNotificationRepo(db *sqlx.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// Create inserts a notification for a user.
func (r *NotificationRepo) Create(ctx context.Context, userID uint64, notifType, title, body string, resourceType *string, resourceID *uint64, resourceRef *string) (uint64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, type, title, body, resource_type, resource_id, resource_ref, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, notifType, title, body, resourceType, resourceID, resourceRef, timeutil.NowUTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("create notification: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read notification id: %w", err)
	}
	return uint64(id), nil
}

func (r *NotificationRepo) CreateDelivery(ctx context.Context, delivery NotificationDelivery) error {
	var errMsg *string
	if delivery.ErrorMessage != "" {
		errMsg = &delivery.ErrorMessage
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_deliveries
		 (notification_type, resource_type, resource_id, user_id, channel, status, attempts, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		delivery.NotificationType, delivery.ResourceType, delivery.ResourceID, delivery.UserID,
		delivery.Channel, delivery.Status, delivery.Attempts, errMsg, timeutil.NowUTC(),
	); err != nil {
		return fmt.Errorf("create notification delivery: %w", err)
	}
	return nil
}

func (r *NotificationRepo) HealthStats(ctx context.Context) (*NotificationHealthStats, error) {
	since := timeutil.NowUTC().Add(-7 * 24 * time.Hour)
	stats := &NotificationHealthStats{}
	if err := r.db.GetContext(ctx, stats,
		`SELECT
		 COALESCE(SUM(CASE WHEN channel = 'lark' AND status = 'failed' THEN 1 ELSE 0 END), 0) AS lark_failed_7d,
		 COALESCE(SUM(CASE WHEN notification_type LIKE '%callback%' AND status = 'failed' THEN 1 ELSE 0 END), 0) AS interactive_callback_failed_7d,
		 COALESCE(SUM(CASE WHEN status = 'failed' OR attempts > 1 THEN 1 ELSE 0 END), 0) AS retry_or_failure_7d,
		 COALESCE(SUM(CASE WHEN notification_type IN ('sso_lark_union_id_missing', 'sso_lark_open_id_missing') THEN 1 ELSE 0 END), 0) AS missing_lark_recipient_7d,
		 COALESCE(SUM(CASE WHEN notification_type IN ('sso_lark_union_id_conflict', 'sso_lark_recipient_conflict') THEN 1 ELSE 0 END), 0) AS recipient_conflict_7d
		 FROM notification_deliveries
		 WHERE created_at >= ?`,
		since,
	); err != nil {
		return nil, fmt.Errorf("load notification health stats: %w", err)
	}
	if err := r.db.SelectContext(ctx, &stats.ByType,
		`SELECT notification_type AS key_name, COUNT(*) AS count
		 FROM notification_deliveries
		 WHERE created_at >= ? AND (status = 'failed' OR attempts > 1)
		 GROUP BY notification_type
		 ORDER BY count DESC LIMIT 8`,
		since,
	); err != nil {
		return nil, fmt.Errorf("count notification failures by type: %w", err)
	}
	return stats, nil
}

// List returns notifications for a user (unread first, then read), most recent first.
func (r *NotificationRepo) List(ctx context.Context, userID uint64, limit, offset int) ([]model.Notification, int, error) {
	var total int
	if err := r.db.GetContext(ctx, &total,
		`SELECT COUNT(*) FROM notifications WHERE user_id = ?`, userID,
	); err != nil {
		return nil, 0, err
	}

	var notifs []model.Notification
	err := r.db.SelectContext(ctx, &notifs,
		`SELECT * FROM notifications WHERE user_id = ? ORDER BY is_read ASC, created_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset,
	)
	return notifs, total, err
}

// UnreadCount returns the number of unread notifications for a user.
func (r *NotificationRepo) UnreadCount(ctx context.Context, userID uint64) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = 0`, userID,
	)
	return count, err
}

// MarkRead marks a single notification as read (only if owned by userID).
func (r *NotificationRepo) MarkRead(ctx context.Context, id, userID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notifications SET is_read = 1 WHERE id = ? AND user_id = ?`, id, userID,
	)
	return err
}

// MarkAllRead marks all unread notifications for a user as read.
func (r *NotificationRepo) MarkAllRead(ctx context.Context, userID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notifications SET is_read = 1 WHERE user_id = ? AND is_read = 0`, userID,
	)
	return err
}

func (r *NotificationRepo) GetByIDForUser(ctx context.Context, id, userID uint64) (*model.Notification, error) {
	var notification model.Notification
	err := r.db.GetContext(ctx, &notification, `SELECT * FROM notifications WHERE id = ? AND user_id = ?`, id, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &notification, nil
}
