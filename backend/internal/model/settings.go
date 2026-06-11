package model

type PlatformSettings struct {
	SensitiveExportReviewerUserIDs      []uint64 `json:"sensitive_export_reviewer_user_ids"`
	SensitiveQueryAccessReviewerUserIDs []uint64 `json:"sensitive_query_access_reviewer_user_ids"`
}
