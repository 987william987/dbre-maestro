import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { ApprovalResolutionWorkflow, PlatformSettings, WorkflowResolution, WorkflowRule, WorkflowRulePreview, WorkflowSimulationRequest } from '@/shared/types/settings'

function normalizeSettings(settings: PlatformSettings): PlatformSettings {
  return {
    app_env: typeof settings.app_env === 'string' ? settings.app_env : '',
    sensitive_export_reviewer_user_ids: Array.isArray(settings.sensitive_export_reviewer_user_ids)
      ? settings.sensitive_export_reviewer_user_ids
      : [],
    sensitive_query_access_reviewer_user_ids: Array.isArray(settings.sensitive_query_access_reviewer_user_ids)
      ? settings.sensitive_query_access_reviewer_user_ids
      : [],
    require_non_sensitive_export_review: typeof settings.require_non_sensitive_export_review === 'boolean'
      ? settings.require_non_sensitive_export_review
      : true,
    approval_policies: Array.isArray(settings.approval_policies)
      ? settings.approval_policies.map((policy) => ({
          workflow_type: policy.workflow_type,
          reviewer_user_ids: Array.isArray(policy.reviewer_user_ids) ? policy.reviewer_user_ids : [],
          reviewer_auth_groups: Array.isArray(policy.reviewer_auth_groups) ? policy.reviewer_auth_groups : [],
          enabled: typeof policy.enabled === 'boolean' ? policy.enabled : true,
        }))
      : [],
    workflow_rules: Array.isArray(settings.workflow_rules)
      ? settings.workflow_rules.map(normalizeWorkflowRule)
      : [],
    lark_app_id: typeof settings.lark_app_id === 'string' ? settings.lark_app_id : '',
    lark_app_secret: typeof settings.lark_app_secret === 'string' ? settings.lark_app_secret : '',
    lark_app_secret_configured: typeof settings.lark_app_secret_configured === 'boolean' ? settings.lark_app_secret_configured : false,
    lark_oauth_enabled: typeof settings.lark_oauth_enabled === 'boolean' ? settings.lark_oauth_enabled : false,
    lark_oauth_site: settings.lark_oauth_site === 'feishu' ? 'feishu' : 'lark',
    lark_oauth_redirect_url: typeof settings.lark_oauth_redirect_url === 'string' ? settings.lark_oauth_redirect_url : '',
    sql_editor_app_timeout_seconds:
      typeof settings.sql_editor_app_timeout_seconds === 'number' ? settings.sql_editor_app_timeout_seconds : 30,
    sql_editor_mysql_max_execution_time_ms:
      typeof settings.sql_editor_mysql_max_execution_time_ms === 'number' ? settings.sql_editor_mysql_max_execution_time_ms : 25000,
    sql_editor_postgres_statement_timeout_ms:
      typeof settings.sql_editor_postgres_statement_timeout_ms === 'number' ? settings.sql_editor_postgres_statement_timeout_ms : 25000,
    db_metadata_inventory_enabled: typeof settings.db_metadata_inventory_enabled === 'boolean' ? settings.db_metadata_inventory_enabled : true,
    db_metadata_inventory_regions: Array.isArray(settings.db_metadata_inventory_regions) ? settings.db_metadata_inventory_regions : [],
    db_metadata_inventory_engines: Array.isArray(settings.db_metadata_inventory_engines) ? settings.db_metadata_inventory_engines : ['aurora-mysql', 'aurora-postgresql', 'redis'],
    db_metadata_inventory_cron: typeof settings.db_metadata_inventory_cron === 'string' ? settings.db_metadata_inventory_cron : '0 9 * * *',
    db_metadata_inventory_sync_interval_minutes:
      typeof settings.db_metadata_inventory_sync_interval_minutes === 'number' ? settings.db_metadata_inventory_sync_interval_minutes : 5,
    db_metadata_object_enabled: typeof settings.db_metadata_object_enabled === 'boolean' ? settings.db_metadata_object_enabled : true,
    db_metadata_object_enabled_connection_ids: Array.isArray(settings.db_metadata_object_enabled_connection_ids)
      ? settings.db_metadata_object_enabled_connection_ids
      : [],
    db_metadata_object_cron: typeof settings.db_metadata_object_cron === 'string' ? settings.db_metadata_object_cron : '0 10 * * *',
    db_metadata_object_sync_interval_minutes:
      typeof settings.db_metadata_object_sync_interval_minutes === 'number' ? settings.db_metadata_object_sync_interval_minutes : 60,
    db_metadata_cron_timezone: typeof settings.db_metadata_cron_timezone === 'string' ? settings.db_metadata_cron_timezone : 'Asia/Taipei',
  }
}

function normalizeWorkflowRule(rule: WorkflowRule): WorkflowRule {
  return {
    id: typeof rule.id === 'number' ? rule.id : undefined,
    rule_name: typeof rule.rule_name === 'string' ? rule.rule_name : '',
    ticket_type: rule.ticket_type,
    db_connection_id: typeof rule.db_connection_id === 'number' ? rule.db_connection_id : null,
    export_sensitivity: rule.export_sensitivity === 'normal' || rule.export_sensitivity === 'sensitive' ? rule.export_sensitivity : null,
    approval_enabled: typeof rule.approval_enabled === 'boolean' ? rule.approval_enabled : true,
    execution_mode: rule.execution_mode === 'auto_after_approval' ? 'auto_after_approval' : 'manual',
    approval_auth_groups: Array.isArray(rule.approval_auth_groups) ? rule.approval_auth_groups : [],
    executor_auth_groups: Array.isArray(rule.executor_auth_groups) ? rule.executor_auth_groups : [],
    priority: typeof rule.priority === 'number' ? rule.priority : 100,
    enabled: typeof rule.enabled === 'boolean' ? rule.enabled : true,
  }
}

export function getSettings() {
  return apiClient.get<PlatformSettings>('/settings').then(normalizeSettings)
}

export function patchSettings(payload: PlatformSettings) {
  return apiClient.patch<PlatformSettings>('/settings', payload).then(normalizeSettings)
}

export function getApprovalResolution() {
  return apiClient.get<{ workflows: ApprovalResolutionWorkflow[] }>('/settings/approval-resolution').then((response) => ({
    workflows: Array.isArray(response.workflows) ? response.workflows : [],
  }))
}

export function listWorkflowRules() {
  return apiClient.get<{ workflow_rules: WorkflowRule[] }>('/settings/workflow-rules').then((response) => ({
    workflow_rules: Array.isArray(response.workflow_rules) ? response.workflow_rules.map(normalizeWorkflowRule) : [],
  }))
}

export function replaceWorkflowRules(workflowRules: WorkflowRule[]) {
  return apiClient.put<{ workflow_rules: WorkflowRule[] }>('/settings/workflow-rules', {
    workflow_rules: workflowRules,
  }).then((response) => ({
    workflow_rules: Array.isArray(response.workflow_rules) ? response.workflow_rules.map(normalizeWorkflowRule) : [],
  }))
}

export function previewWorkflowRule(rule: WorkflowRule) {
  return apiClient.post<{ workflow_resolution: WorkflowResolution }>('/settings/workflow-rules/preview', rule)
}

export function previewWorkflowRules(workflowRules: WorkflowRule[]) {
  return apiClient.post<{ previews: WorkflowRulePreview[] }>('/settings/workflow-rules/effective-preview', {
    workflow_rules: workflowRules,
  }).then((response) => ({
    previews: Array.isArray(response.previews) ? response.previews : [],
  }))
}

export function simulateWorkflowRule(payload: WorkflowSimulationRequest) {
  return apiClient.post<{ workflow_resolution: WorkflowResolution }>('/settings/workflow-rules/simulate', payload)
}

type SettingsDBConnectionsResponse = {
  connections: Array<Pick<DBConnection, 'id' | 'name' | 'db_type' | 'host' | 'port'>>
}

export function listSettingsDBConnections() {
  return apiClient.get<SettingsDBConnectionsResponse>('/settings/db-connections').then((response) => ({
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }))
}
