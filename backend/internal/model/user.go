package model

import "time"

type AuthGroup string

const (
	AuthGroupDeveloper AuthGroup = "developer"
	AuthGroupReviewer  AuthGroup = "reviewer"
	AuthGroupDBA       AuthGroup = "dba"
	AuthGroupAdmin     AuthGroup = "admin"
	AuthGroupSecurity  AuthGroup = "security"
	AuthGroupDataOwner AuthGroup = "data_owner"
)

type User struct {
	ID                     uint64     `db:"id"`
	Username               string     `db:"username"`
	Email                  string     `db:"email"`
	LarkRecipient          string     `db:"lark_recipient"`
	LarkRecipientType      string     `db:"lark_recipient_type"`
	LarkUnionID            string     `db:"lark_union_id"`
	Password               string     `db:"password"`
	ExternalIdentitySource string     `db:"external_identity_source"`
	ExternalIdentityID     string     `db:"external_identity_id"`
	PasswordLoginDisabled  bool       `db:"password_login_disabled"`
	LarkLoginOpenID        string     `db:"lark_login_open_id"`
	LarkLoginUnionID       string     `db:"lark_login_union_id"`
	LarkDisplayName        string     `db:"lark_display_name"`
	LarkAvatarURL          string     `db:"lark_avatar_url"`
	LarkBoundAt            *time.Time `db:"lark_bound_at"`
	LarkBindingStatus      string     `db:"lark_binding_status"`
	IsSetup                bool       `db:"is_setup"`
	IsProtected            bool       `db:"is_protected"`
	IsActive               bool       `db:"is_active"`
	MFAEnabled             bool       `db:"mfa_enabled"`
	MFASecret              []byte     `db:"mfa_secret_encrypted"`
	MFAEnabledAt           *time.Time `db:"mfa_enabled_at"`
	CreatedAt              time.Time  `db:"created_at"`
	UpdatedAt              time.Time  `db:"updated_at"`
}

type Membership struct {
	ID        uint64     `db:"id" json:"id"`
	UserID    uint64     `db:"user_id" json:"user_id"`
	AuthGroup AuthGroup  `db:"auth_group" json:"auth_group"`
	GrantedBy *uint64    `db:"granted_by" json:"granted_by"`
	ExpiresAt *time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
}
