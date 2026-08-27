package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type UserRepo struct {
	db     *sqlx.DB
	encKey []byte
}

type AuthGroupRecord struct {
	ID          uint64 `db:"id"`
	GroupKey    string `db:"group_key"`
	Name        string `db:"name"`
	IsSystem    bool   `db:"is_system"`
	IsProtected bool   `db:"is_protected"`
}

type ResourceBoundUser struct {
	ID       uint64 `db:"id" json:"id"`
	Username string `db:"username" json:"username"`
}

type LarkIdentityInput struct {
	OpenID       string
	UnionID      string
	Email        string
	DisplayName  string
	AvatarURL    string
	PasswordHash string
}

type SSOIdentityInput struct {
	Provider     string
	Subject      string
	Email        string
	DisplayName  string
	LarkOpenID   string
	LarkUnionID  string
	PasswordHash string
}

func NewUserRepo(db *sqlx.DB, encKey ...[]byte) *UserRepo {
	var key []byte
	if len(encKey) > 0 {
		key = encKey[0]
	}
	return &UserRepo{db: db, encKey: key}
}

func (r *UserRepo) GetByLarkLoginIdentity(ctx context.Context, openID, unionID string) (*model.User, error) {
	openID = strings.TrimSpace(openID)
	unionID = strings.TrimSpace(unionID)
	if openID == "" && unionID == "" {
		return nil, nil
	}

	var u model.User
	var err error
	switch {
	case openID != "" && unionID != "":
		err = r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE lark_login_open_id = ? OR lark_login_union_id = ? ORDER BY id LIMIT 1`, openID, unionID)
	case openID != "":
		err = r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE lark_login_open_id = ? ORDER BY id LIMIT 1`, openID)
	default:
		err = r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE lark_login_union_id = ? ORDER BY id LIMIT 1`, unionID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) GetByLarkOperator(ctx context.Context, openID, unionID string) (*model.User, error) {
	openID = strings.TrimSpace(openID)
	unionID = strings.TrimSpace(unionID)
	if openID == "" && unionID == "" {
		return nil, nil
	}

	conditions := []string{}
	args := []any{}
	if unionID != "" {
		conditions = append(conditions, "lark_union_id = ?", "lark_login_union_id = ?")
		args = append(args, unionID, unionID)
	}
	if openID != "" {
		conditions = append(conditions, "lark_login_open_id = ?", "(lark_recipient_type = 'open_id' AND lark_recipient = ?)")
		args = append(args, openID, openID)
	}
	query := `SELECT * FROM users WHERE is_active = 1 AND (` + strings.Join(conditions, " OR ") + `) ORDER BY id LIMIT 1`
	var u model.User
	err := r.db.GetContext(ctx, &u, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) GetByExternalIdentity(ctx context.Context, provider, subject string) (*model.User, error) {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	if provider == "" || subject == "" {
		return nil, nil
	}
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE external_identity_source = ? AND external_identity_id = ? ORDER BY id LIMIT 1`, provider, subject)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) BindExternalIdentity(ctx context.Context, userID uint64, provider, subject string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET external_identity_source = ?, external_identity_id = ?, password_login_disabled = 1, updated_at = ?
		WHERE id = ?
	`, strings.TrimSpace(provider), strings.TrimSpace(subject), timeutil.NowUTC(), userID)
	return err
}

func (r *UserRepo) UpdateLarkUnionID(ctx context.Context, userID uint64, larkUnionID string) (bool, error) {
	larkUnionID = strings.TrimSpace(larkUnionID)
	if larkUnionID == "" {
		return false, nil
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET lark_union_id = ?, lark_recipient_type = 'union_id', updated_at = ?
		WHERE id = ? AND (lark_union_id = '' OR (lark_union_id = ? AND lark_recipient_type <> 'union_id'))
	`, larkUnionID, timeutil.NowUTC(), userID, larkUnionID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *UserRepo) BindLarkIdentity(ctx context.Context, userID uint64, identity LarkIdentityInput) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET lark_recipient = ?,
		    lark_recipient_type = 'open_id',
		    external_identity_source = 'lark',
		    external_identity_id = ?,
		    lark_login_open_id = ?,
		    lark_login_union_id = ?,
		    lark_display_name = ?,
		    lark_avatar_url = ?,
		    lark_bound_at = ?,
		    lark_binding_status = 'bound',
		    updated_at = ?
		WHERE id = ?
	`, strings.TrimSpace(identity.OpenID), firstNonEmptyUserValue(identity.UnionID, identity.OpenID),
		strings.TrimSpace(identity.OpenID), strings.TrimSpace(identity.UnionID),
		strings.TrimSpace(identity.DisplayName), strings.TrimSpace(identity.AvatarURL),
		now, now, userID)
	return err
}

func (r *UserRepo) CreateLarkDeveloper(ctx context.Context, username string, identity LarkIdentityInput) (*model.User, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := timeutil.NowUTC()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO users (
		    username, email, lark_recipient, lark_recipient_type, password,
		    external_identity_source, external_identity_id, password_login_disabled,
		    lark_login_open_id, lark_login_union_id, lark_display_name, lark_avatar_url,
		    lark_bound_at, lark_binding_status,
		    is_protected, is_active, created_at, updated_at
		) VALUES (?, ?, ?, 'open_id', ?, 'lark', ?, 1, ?, ?, ?, ?, ?, 'bound', 0, 1, ?, ?)
	`, username, strings.TrimSpace(identity.Email), strings.TrimSpace(identity.OpenID), identity.PasswordHash,
		firstNonEmptyUserValue(identity.UnionID, identity.OpenID), strings.TrimSpace(identity.OpenID), strings.TrimSpace(identity.UnionID),
		strings.TrimSpace(identity.DisplayName), strings.TrimSpace(identity.AvatarURL), now, now, now)
	if err != nil {
		return nil, fmt.Errorf("create lark user: %w", err)
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at, created_at)
		VALUES (?, ?, NULL, NULL, ?)
		ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at), granted_by = VALUES(granted_by)
	`, id, model.AuthGroupDeveloper, now); err != nil {
		return nil, fmt.Errorf("grant developer group: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, uint64(id))
}

func (r *UserRepo) CreateSSODeveloper(ctx context.Context, username string, identity SSOIdentityInput) (*model.User, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := timeutil.NowUTC()
	larkRecipientType := "open_id"
	if strings.TrimSpace(identity.LarkUnionID) != "" {
		larkRecipientType = "union_id"
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO users (
		    username, email, lark_recipient, lark_recipient_type, lark_union_id, password,
		    external_identity_source, external_identity_id, password_login_disabled,
		    is_protected, is_active, created_at, updated_at
		) VALUES (?, ?, '', ?, ?, ?, ?, ?, 1, 0, 1, ?, ?)
	`, username, strings.TrimSpace(identity.Email), larkRecipientType, strings.TrimSpace(identity.LarkUnionID), identity.PasswordHash,
		strings.TrimSpace(identity.Provider), strings.TrimSpace(identity.Subject), now, now)
	if err != nil {
		return nil, fmt.Errorf("create sso user: %w", err)
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at, created_at)
		VALUES (?, ?, NULL, NULL, ?)
		ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at), granted_by = VALUES(granted_by)
	`, id, model.AuthGroupDeveloper, now); err != nil {
		return nil, fmt.Errorf("grant developer group: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, uint64(id))
}

func (r *UserRepo) Create(ctx context.Context, username, email, larkRecipient, larkRecipientType, larkUnionID, passwordHash string, isProtected bool) (*model.User, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (username, email, lark_recipient, lark_recipient_type, lark_union_id, password, is_protected, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		username, email, strings.TrimSpace(larkRecipient), normalizeLarkRecipientType(larkRecipientType), strings.TrimSpace(larkUnionID), passwordHash, isProtected, timeutil.NowUTC(), timeutil.NowUTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *UserRepo) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE username = ?`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE email = ?`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) ListByIDs(ctx context.Context, ids []uint64) ([]model.User, error) {
	if len(ids) == 0 {
		return []model.User{}, nil
	}
	query, args, err := sqlx.In(`SELECT * FROM users WHERE id IN (?) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	var users []model.User
	if err := r.db.SelectContext(ctx, &users, query, args...); err != nil {
		return nil, err
	}
	return users, nil
}

func (r *UserRepo) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM users`)
	return count, err
}

func (r *UserRepo) GetAuthGroups(ctx context.Context, userID uint64) ([]model.AuthGroup, error) {
	var groups []model.AuthGroup
	err := r.db.SelectContext(ctx, &groups, `
		SELECT DISTINCT auth_group
		FROM (
			SELECT ag.group_key AS auth_group
			FROM user_auth_groups uag
			INNER JOIN auth_groups ag ON ag.id = uag.auth_group_id
			WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT agm.auth_group
			FROM auth_group_memberships agm
			WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		) AS membership_groups
		ORDER BY auth_group
	`, userID, timeutil.NowUTC(), userID, timeutil.NowUTC())
	return groups, err
}

func (r *UserRepo) RequiresMFA(ctx context.Context, user *model.User) (bool, error) {
	if user == nil {
		return false, nil
	}
	if user.IsProtected {
		return true, nil
	}
	groups, err := r.GetAuthGroups(ctx, user.ID)
	if err != nil {
		return false, err
	}
	for _, group := range groups {
		if group == model.AuthGroupAdmin {
			return true, nil
		}
	}
	return false, nil
}

func (r *UserRepo) StoreMFASecret(ctx context.Context, userID uint64, secret string) error {
	if len(r.encKey) == 0 {
		return errors.New("user mfa encryption key is not configured")
	}
	encrypted, err := crypto.Encrypt(r.encKey, []byte(secret))
	if err != nil {
		return fmt.Errorf("encrypt mfa secret: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE users SET mfa_secret_encrypted = ?, mfa_enabled = 0, mfa_enabled_at = NULL, updated_at = ? WHERE id = ?`,
		encrypted, timeutil.NowUTC(), userID,
	)
	return err
}

func (r *UserRepo) DecryptMFASecret(user *model.User) (string, error) {
	if user == nil || len(user.MFASecret) == 0 {
		return "", nil
	}
	if len(r.encKey) == 0 {
		return "", errors.New("user mfa encryption key is not configured")
	}
	plain, err := crypto.Decrypt(r.encKey, user.MFASecret)
	if err != nil {
		return "", fmt.Errorf("decrypt mfa secret: %w", err)
	}
	return string(plain), nil
}

func (r *UserRepo) EnableMFA(ctx context.Context, userID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET mfa_enabled = 1, mfa_enabled_at = ?, updated_at = ? WHERE id = ? AND mfa_secret_encrypted IS NOT NULL`,
		timeutil.NowUTC(), timeutil.NowUTC(), userID,
	)
	return err
}

func (r *UserRepo) ResetMFA(ctx context.Context, userID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET mfa_enabled = 0, mfa_secret_encrypted = NULL, mfa_enabled_at = NULL, updated_at = ? WHERE id = ?`,
		timeutil.NowUTC(), userID,
	)
	return err
}

func (r *UserRepo) GetAuthGroupRecords(ctx context.Context, userID uint64) ([]AuthGroupRecord, error) {
	var groups []AuthGroupRecord
	err := r.db.SelectContext(ctx, &groups, `
		SELECT DISTINCT ag.id, ag.group_key, ag.name, ag.is_system, ag.is_protected
		FROM auth_groups ag
		INNER JOIN user_auth_groups uag ON uag.auth_group_id = ag.id
		WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
		ORDER BY ag.id
	`, userID, timeutil.NowUTC())
	if err == nil && len(groups) > 0 {
		return groups, nil
	}

	err = r.db.SelectContext(ctx, &groups, `
		SELECT DISTINCT ag.id, ag.group_key, ag.name, ag.is_system, ag.is_protected
		FROM auth_groups ag
		INNER JOIN auth_group_memberships agm ON agm.auth_group = ag.group_key
		WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		ORDER BY ag.id
	`, userID, timeutil.NowUTC())
	return groups, err
}

func (r *UserRepo) GetEffectiveAuthGroupIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.db.SelectContext(ctx, &ids, `
		SELECT DISTINCT id FROM (
			SELECT ag.id
			FROM auth_groups ag
			INNER JOIN user_auth_groups uag ON uag.auth_group_id = ag.id
			WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT ag.id
			FROM auth_groups ag
			INNER JOIN auth_group_memberships agm ON agm.auth_group = ag.group_key
			WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		) AS effective_auth_groups
		ORDER BY id
	`, userID, timeutil.NowUTC(), userID, timeutil.NowUTC())
	return ids, err
}

func (r *UserRepo) GetEffectivePermissionKeys(ctx context.Context, userID uint64) ([]string, error) {
	hasAllPermissions, err := r.HasAllPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if hasAllPermissions {
		var permissionKeys []string
		err := r.db.SelectContext(ctx, &permissionKeys, `SELECT permission_key FROM permissions ORDER BY permission_key`)
		return permissionKeys, err
	}

	var permissionKeys []string
	err = r.db.SelectContext(ctx, &permissionKeys, `
		SELECT DISTINCT permission_key FROM (
			SELECT p.permission_key
			FROM permissions p
			INNER JOIN user_permissions up ON up.permission_id = p.id
			WHERE up.user_id = ?
			UNION
			SELECT p.permission_key
			FROM permissions p
			INNER JOIN auth_group_permissions agp ON agp.permission_id = p.id
			INNER JOIN user_auth_groups uag ON uag.auth_group_id = agp.auth_group_id
			WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT p.permission_key
			FROM permissions p
			INNER JOIN auth_group_permissions agp ON agp.permission_id = p.id
			INNER JOIN auth_groups ag ON ag.id = agp.auth_group_id
			INNER JOIN auth_group_memberships agm ON agm.auth_group = ag.group_key
			WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		) AS effective_permissions
		ORDER BY permission_key
	`, userID, userID, timeutil.NowUTC(), userID, timeutil.NowUTC())
	return permissionKeys, err
}

func (r *UserRepo) HasAllPermissions(ctx context.Context, userID uint64) (bool, error) {
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user != nil && user.IsProtected {
		return true, nil
	}

	var hasAllPermissions bool
	err = r.db.GetContext(ctx, &hasAllPermissions, `
		SELECT EXISTS (
			SELECT 1 FROM auth_groups ag
			WHERE ag.is_all_permissions = 1
			  AND (
			    EXISTS (
			      SELECT 1 FROM user_auth_groups uag
			      WHERE uag.auth_group_id = ag.id AND uag.user_id = ?
			        AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			    )
			    OR EXISTS (
			      SELECT 1 FROM auth_group_memberships agm
			      WHERE agm.auth_group = ag.group_key AND agm.user_id = ?
			        AND (agm.expires_at IS NULL OR agm.expires_at > ?)
			    )
			  )
		)
	`, userID, timeutil.NowUTC(), userID, timeutil.NowUTC())
	return hasAllPermissions, err
}

func (r *UserRepo) ListActiveUserIDsByPermissionKeys(ctx context.Context, permissionKeys []string) ([]uint64, error) {
	if len(permissionKeys) == 0 {
		return []uint64{}, nil
	}

	now := timeutil.NowUTC()
	query, args, err := sqlx.In(`
		SELECT DISTINCT u.id
		FROM users u
		WHERE u.is_active = 1
		  AND u.id IN (
			SELECT up.user_id
			FROM user_permissions up
			INNER JOIN permissions p ON p.id = up.permission_id
			WHERE p.permission_key IN (?)
			UNION
			SELECT uag.user_id
			FROM user_auth_groups uag
			INNER JOIN auth_group_permissions agp ON agp.auth_group_id = uag.auth_group_id
			INNER JOIN permissions p ON p.id = agp.permission_id
			WHERE p.permission_key IN (?)
			  AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT agm.user_id
			FROM auth_group_memberships agm
			INNER JOIN auth_groups ag ON ag.group_key = agm.auth_group
			INNER JOIN auth_group_permissions agp ON agp.auth_group_id = ag.id
			INNER JOIN permissions p ON p.id = agp.permission_id
			WHERE p.permission_key IN (?)
			  AND (agm.expires_at IS NULL OR agm.expires_at > ?)
			UNION
			SELECT id FROM users WHERE is_protected = 1 AND is_active = 1
			UNION
			SELECT uag2.user_id
			FROM user_auth_groups uag2
			INNER JOIN auth_groups ag2 ON ag2.id = uag2.auth_group_id
			WHERE ag2.is_all_permissions = 1
			  AND (uag2.expires_at IS NULL OR uag2.expires_at > ?)
			UNION
			SELECT agm2.user_id
			FROM auth_group_memberships agm2
			INNER JOIN auth_groups ag3 ON ag3.group_key = agm2.auth_group
			WHERE ag3.is_all_permissions = 1
			  AND (agm2.expires_at IS NULL OR agm2.expires_at > ?)
		  )
		ORDER BY u.id
	`, permissionKeys, permissionKeys, now, permissionKeys, now, now, now)
	if err != nil {
		return nil, err
	}

	query = r.db.Rebind(query)
	var userIDs []uint64
	if err := r.db.SelectContext(ctx, &userIDs, query, args...); err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (r *UserRepo) GetEffectiveDBConnectionIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	hasAllPermissions, err := r.HasAllPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if hasAllPermissions {
		var allIDs []uint64
		if err := r.db.SelectContext(ctx, &allIDs, `
			SELECT id
			FROM db_connections
			WHERE deleted_at IS NULL
			ORDER BY id
		`); err != nil {
			return nil, err
		}
		return allIDs, nil
	}

	var ids []uint64
	err = r.db.SelectContext(ctx, &ids, `
		SELECT DISTINCT db_connection_id FROM (
			SELECT udc.db_connection_id
			FROM user_db_connections udc
			INNER JOIN db_connections dc ON dc.id = udc.db_connection_id AND dc.deleted_at IS NULL
			WHERE udc.user_id = ?
			UNION
			SELECT agdc.db_connection_id
			FROM auth_group_db_connections agdc
			INNER JOIN db_connections dc ON dc.id = agdc.db_connection_id AND dc.deleted_at IS NULL
			INNER JOIN user_auth_groups uag ON uag.auth_group_id = agdc.auth_group_id
			WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT agdc.db_connection_id
			FROM auth_group_db_connections agdc
			INNER JOIN db_connections dc ON dc.id = agdc.db_connection_id AND dc.deleted_at IS NULL
			INNER JOIN auth_groups ag ON ag.id = agdc.auth_group_id
			INNER JOIN auth_group_memberships agm ON agm.auth_group = ag.group_key
			WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		) AS effective_db_connections
		ORDER BY db_connection_id
	`, userID, userID, timeutil.NowUTC(), userID, timeutil.NowUTC())
	return ids, err
}

func (r *UserRepo) AddMembership(ctx context.Context, userID uint64, group model.AuthGroup, grantedBy *uint64, expiresAt *time.Time) error {
	now := timeutil.NowUTC()
	var expiry any
	if expiresAt != nil {
		expiry = expiresAt.UTC()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at, created_at) VALUES (?, ?, ?, ?, ?)
         ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at), granted_by = VALUES(granted_by)`,
		userID, group, grantedBy, expiry, now,
	)
	return err
}

func (r *UserRepo) RemoveMembership(ctx context.Context, userID uint64, group model.AuthGroup) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_group_memberships WHERE user_id = ? AND auth_group = ?`, userID, group); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE uag FROM user_auth_groups uag
		INNER JOIN auth_groups ag ON ag.id = uag.auth_group_id
		WHERE uag.user_id = ? AND ag.group_key = ?
	`, userID, group); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *UserRepo) AddDirectPermission(ctx context.Context, userID uint64, permissionKey string, grantedBy *uint64) error {
	now := timeutil.NowUTC()
	res, err := r.db.ExecContext(ctx, `
		INSERT IGNORE INTO user_permissions (user_id, permission_id, granted_by, created_at)
		SELECT ?, p.id, ?, ?
		FROM permissions p
		WHERE p.permission_key = ?
	`, userID, grantedBy, now, permissionKey)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		var exists int
		if err := r.db.GetContext(ctx, &exists, `SELECT COUNT(*) FROM permissions WHERE permission_key = ?`, permissionKey); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

func (r *UserRepo) RemoveDirectPermission(ctx context.Context, userID uint64, permissionKey string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE up FROM user_permissions up
		INNER JOIN permissions p ON p.id = up.permission_id
		WHERE up.user_id = ? AND p.permission_key = ?
	`, userID, permissionKey)
	return err
}

func (r *UserRepo) AddDirectDBConnection(ctx context.Context, userID, dbConnectionID uint64, grantedBy *uint64) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT IGNORE INTO user_db_connections (user_id, db_connection_id, granted_by, created_at)
		VALUES (?, ?, ?, ?)
	`, userID, dbConnectionID, grantedBy, now)
	return err
}

func (r *UserRepo) RemoveDirectDBConnection(ctx context.Context, userID, dbConnectionID uint64) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM user_db_connections
		WHERE user_id = ? AND db_connection_id = ?
	`, userID, dbConnectionID)
	return err
}

func (r *UserRepo) ListDirectPermissionKeys(ctx context.Context, userID uint64) ([]string, error) {
	var permissionKeys []string
	err := r.db.SelectContext(ctx, &permissionKeys, `
		SELECT p.permission_key
		FROM permissions p
		INNER JOIN user_permissions up ON up.permission_id = p.id
		WHERE up.user_id = ?
		ORDER BY p.permission_key
	`, userID)
	return permissionKeys, err
}

func (r *UserRepo) ListDirectDBConnectionIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.db.SelectContext(ctx, &ids, `
		SELECT db_connection_id
		FROM user_db_connections
		WHERE user_id = ?
		ORDER BY db_connection_id
	`, userID)
	return ids, err
}

// Update patches username, email, and lark notification recipient. Call separately to update password hash.
func (r *UserRepo) Update(ctx context.Context, id uint64, username, email, larkRecipient, larkRecipientType, larkUnionID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET username=?, email=?, lark_recipient=?, lark_recipient_type=?, lark_union_id=?, updated_at=? WHERE id=?`,
		username, email, strings.TrimSpace(larkRecipient), normalizeLarkRecipientType(larkRecipientType), strings.TrimSpace(larkUnionID), timeutil.NowUTC(), id,
	)
	return err
}

func normalizeLarkRecipientType(value string) string {
	if strings.TrimSpace(value) == "union_id" {
		return "union_id"
	}
	return "open_id"
}

func (r *UserRepo) UpdateActive(ctx context.Context, id uint64, isActive bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET is_active=?, updated_at=? WHERE id=?`,
		isActive, timeutil.NowUTC(), id,
	)
	return err
}

func (r *UserRepo) UpdatePassword(ctx context.Context, id uint64, passwordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password=?, updated_at=? WHERE id=?`,
		passwordHash, timeutil.NowUTC(), id,
	)
	return err
}

func (r *UserRepo) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := r.db.SelectContext(ctx, &users, `SELECT * FROM users ORDER BY created_at DESC`)
	return users, err
}

func (r *UserRepo) ListMemberships(ctx context.Context, userID uint64) ([]model.Membership, error) {
	var memberships []model.Membership
	err := r.db.SelectContext(ctx, &memberships, `
		SELECT membership_id AS id, user_id, auth_group, granted_by, expires_at, created_at
		FROM (
			SELECT
				uag.id AS membership_id,
				uag.user_id,
				ag.group_key AS auth_group,
				uag.granted_by,
				uag.expires_at,
				uag.created_at
			FROM user_auth_groups uag
			INNER JOIN auth_groups ag ON ag.id = uag.auth_group_id
			WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT
				agm.id AS membership_id,
				agm.user_id,
				agm.auth_group,
				agm.granted_by,
				agm.expires_at,
				agm.created_at
			FROM auth_group_memberships agm
			WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		) AS memberships
		ORDER BY created_at DESC
	`, userID, timeutil.NowUTC(), userID, timeutil.NowUTC())
	return memberships, err
}

func (r *UserRepo) Delete(ctx context.Context, id uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM auth_group_memberships WHERE user_id = ?`,
			args:  []any{id},
		},
		{
			query: `DELETE FROM resource_group_users WHERE user_id = ?`,
			args:  []any{id},
		},
		{
			query: `DELETE FROM sessions WHERE user_id = ?`,
			args:  []any{id},
		},
		{
			query: `DELETE FROM users WHERE id = ?`,
			args:  []any{id},
		},
	}

	for _, item := range queries {
		if _, err := tx.ExecContext(ctx, item.query, item.args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func firstNonEmptyUserValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *UserRepo) ListUsersByAuthGroup(ctx context.Context, group model.AuthGroup) ([]model.User, error) {
	var users []model.User
	err := r.db.SelectContext(ctx, &users, `
		SELECT DISTINCT u.*
		FROM users u
		WHERE u.id IN (
			SELECT uag.user_id
			FROM user_auth_groups uag
			INNER JOIN auth_groups ag ON ag.id = uag.auth_group_id
			WHERE ag.group_key = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT agm.user_id
			FROM auth_group_memberships agm
			WHERE agm.auth_group = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		)
		ORDER BY u.username
	`, group, timeutil.NowUTC(), group, timeutil.NowUTC())
	return users, err
}

func (r *UserRepo) ListDirectUsersByDBConnection(ctx context.Context, dbConnectionID uint64) ([]ResourceBoundUser, error) {
	var users []ResourceBoundUser
	err := r.db.SelectContext(ctx, &users, `
		SELECT DISTINCT u.id, u.username
		FROM users u
		INNER JOIN user_db_connections udc ON udc.user_id = u.id
		WHERE udc.db_connection_id = ?
		ORDER BY u.username
	`, dbConnectionID)
	return users, err
}

func (r *UserRepo) ListEffectiveUsersByDBConnection(ctx context.Context, dbConnectionID uint64) ([]ResourceBoundUser, error) {
	var users []ResourceBoundUser
	now := timeutil.NowUTC()
	err := r.db.SelectContext(ctx, &users, `
		SELECT DISTINCT u.id, u.username
		FROM users u
		WHERE u.id IN (
			SELECT udc.user_id
			FROM user_db_connections udc
			WHERE udc.db_connection_id = ?
			UNION
			SELECT uag.user_id
			FROM auth_group_db_connections agdc
			INNER JOIN user_auth_groups uag ON uag.auth_group_id = agdc.auth_group_id
			WHERE agdc.db_connection_id = ?
			  AND (uag.expires_at IS NULL OR uag.expires_at > ?)
			UNION
			SELECT agm.user_id
			FROM auth_group_db_connections agdc
			INNER JOIN auth_groups ag ON ag.id = agdc.auth_group_id
			INNER JOIN auth_group_memberships agm ON agm.auth_group = ag.group_key
			WHERE agdc.db_connection_id = ?
			  AND (agm.expires_at IS NULL OR agm.expires_at > ?)
		)
		ORDER BY u.username
	`, dbConnectionID, dbConnectionID, now, dbConnectionID, now)
	return users, err
}

func (r *UserRepo) ReplaceMemberships(ctx context.Context, userID uint64, groups []model.AuthGroup, grantedBy *uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_group_memberships WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_auth_groups WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, group := range groups {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at, created_at)
			VALUES (?, ?, ?, NULL, ?)
		`, userID, group, grantedBy, timeutil.NowUTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *UserRepo) ReplaceDirectPermissionKeys(ctx context.Context, userID uint64, permissionKeys []string, grantedBy *uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_permissions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, permissionKey := range permissionKeys {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO user_permissions (user_id, permission_id, granted_by, created_at)
			SELECT ?, p.id, ?, ?
			FROM permissions p
			WHERE p.permission_key = ?
		`, userID, grantedBy, timeutil.NowUTC(), permissionKey)
		if err != nil {
			return err
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}

func (r *UserRepo) ReplaceDirectDBConnectionIDs(ctx context.Context, userID uint64, dbConnectionIDs []uint64, grantedBy *uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_db_connections WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, connectionID := range dbConnectionIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_db_connections (user_id, db_connection_id, granted_by, created_at)
			VALUES (?, ?, ?, ?)
		`, userID, connectionID, grantedBy, timeutil.NowUTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
