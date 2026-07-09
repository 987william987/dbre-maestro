package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/dbre-maestro/maestro/internal/config"
)

type SecretGetter interface {
	GetSecretValue(ctx context.Context, secretID string) (string, error)
}

type AWSSecretsManagerGetter struct {
	client *secretsmanager.Client
}

func NewAWSSecretsManagerGetter(ctx context.Context, region string) (*AWSSecretsManagerGetter, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &AWSSecretsManagerGetter{client: secretsmanager.NewFromConfig(awsCfg)}, nil
}

func (getter *AWSSecretsManagerGetter) GetSecretValue(ctx context.Context, secretID string) (string, error) {
	output, err := getter.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(secretID)})
	if err != nil {
		return "", fmt.Errorf("read aws secret: %w", err)
	}
	if output.SecretString != nil {
		return *output.SecretString, nil
	}
	if len(output.SecretBinary) > 0 {
		return string(output.SecretBinary), nil
	}
	return "", errors.New("aws secret has no string or binary value")
}

func LoadApplicationSecretsFromAWS(ctx context.Context, cfg *config.Config) error {
	if cfg == nil || !cfg.AWSSecretsManagerEnabled {
		return nil
	}
	if err := validateAWSSecretsManagerConfig(cfg); err != nil {
		return err
	}
	getter, err := NewAWSSecretsManagerGetter(ctx, cfg.AWSSecretsManagerRegion)
	if err != nil {
		return err
	}
	return ResolveApplicationSecrets(ctx, cfg, getter)
}

func ResolveApplicationSecrets(ctx context.Context, cfg *config.Config, getter SecretGetter) error {
	if cfg == nil || !cfg.AWSSecretsManagerEnabled {
		return nil
	}
	if err := validateAWSSecretsManagerConfig(cfg); err != nil {
		return err
	}
	if getter == nil {
		return errors.New("aws secrets manager client is required")
	}

	secret, err := getter.GetSecretValue(ctx, cfg.AWSSecretsManagerSecretID)
	if err != nil {
		return fmt.Errorf("load application secret from aws secrets manager: %w", err)
	}
	values, err := applicationSecretsFromPayload(secret)
	if err != nil {
		return err
	}
	if err := applyApplicationSecrets(cfg, values); err != nil {
		return err
	}
	return cfg.ValidateRequiredSecrets()
}

func validateAWSSecretsManagerConfig(cfg *config.Config) error {
	if strings.TrimSpace(cfg.AWSSecretsManagerRegion) == "" {
		return errors.New("AWS_SM_REGION is required when AWS_SM_ENABLE=true")
	}
	if strings.TrimSpace(cfg.AWSSecretsManagerSecretID) == "" {
		return errors.New("AWS_SM_SECRET_ID is required when AWS_SM_ENABLE=true")
	}
	return nil
}

type applicationSecretPayload struct {
	DBDSN             string `json:"DB_DSN"`
	MigrationDSN      string `json:"MIGRATION_DSN"`
	DBREEncryptionKey string `json:"DBRE_ENCRYPTION_KEY"`
	JWTSecret         string `json:"JWT_SECRET"`
	LarkAppID         string `json:"LARK_APP_ID"`
	LarkAppSecret     string `json:"LARK_APP_SECRET"`
}

func applicationSecretsFromPayload(secret string) (applicationSecretPayload, error) {
	var payload applicationSecretPayload
	if err := json.Unmarshal([]byte(secret), &payload); err != nil {
		return applicationSecretPayload{}, errors.New("application secret is not valid json")
	}
	missing := missingApplicationSecretFields(payload)
	if len(missing) > 0 {
		return applicationSecretPayload{}, fmt.Errorf("application secret is missing required fields: %s", strings.Join(missing, ", "))
	}
	return payload, nil
}

func missingApplicationSecretFields(payload applicationSecretPayload) []string {
	var missing []string
	if strings.TrimSpace(payload.DBDSN) == "" {
		missing = append(missing, "DB_DSN")
	}
	if strings.TrimSpace(payload.DBREEncryptionKey) == "" {
		missing = append(missing, "DBRE_ENCRYPTION_KEY")
	}
	if strings.TrimSpace(payload.JWTSecret) == "" {
		missing = append(missing, "JWT_SECRET")
	}
	return missing
}

func applyApplicationSecrets(cfg *config.Config, values applicationSecretPayload) error {
	if value := strings.TrimSpace(values.DBDSN); value != "" {
		cfg.DBDSN = value
	}
	if value := strings.TrimSpace(values.MigrationDSN); value != "" {
		cfg.MigrationDSN = value
	}
	if value := strings.TrimSpace(values.JWTSecret); value != "" {
		cfg.JWTSecret = []byte(value)
	}
	if value := strings.TrimSpace(values.DBREEncryptionKey); value != "" {
		if err := cfg.SetEncryptionKey(value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(values.LarkAppID); value != "" {
		cfg.LarkOAuth.AppID = value
	}
	if value := strings.TrimSpace(values.LarkAppSecret); value != "" {
		cfg.LarkOAuth.AppSecret = value
	}
	return nil
}
