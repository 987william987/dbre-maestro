package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

func NewNotificationRepo(db *sqlx.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// Create inserts a notification for a user.
func (r *NotificationRepo) Create(ctx context.Context, userID uint64, notifType, title, body string, resourceType *string, resourceID *uint64) (uint64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, type, title, body, resource_type, resource_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, notifType, title, body, resourceType, resourceID, timeutil.NowUTC(),
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
