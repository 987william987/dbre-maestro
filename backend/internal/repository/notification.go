package repository

import (
	"context"
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type NotificationRepo struct {
	db *sqlx.DB
}

func NewNotificationRepo(db *sqlx.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// Create inserts a notification for a user.
func (r *NotificationRepo) Create(ctx context.Context, userID uint64, notifType, title, body string, resourceType *string, resourceID *uint64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, type, title, body, resource_type, resource_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, notifType, title, body, resourceType, resourceID, timeutil.NowUTC(),
	)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
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
