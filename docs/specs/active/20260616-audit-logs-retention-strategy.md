---
status: active
spec_issue_number:
spec_issue_url:
spec_filed_at: 2026-06-16T00:00:00+08:00
spec_branch: dev-william
spec_plan_mode: inactive
spec_executed: false
spec_worktree_path:
ttfc_ms:
tthw_ms:
---

# Audit Logs 長期保留與清理策略

## Context

目前 `audit_logs` 已是平台的重要治理資料來源，承載：

- 操作稽核
- 權限與設定變更追蹤
- 事故與安全事件回溯

現階段功能已可用，但 `audit_logs` 尚未有明確的長期 retention 與清理策略。當資料量持續累積時，最直接的風險不是功能錯誤，而是：

- 查詢與匯出速度下降
- 備份與還原成本升高
- migration / schema 維護成本增加
- 未來難以在不停機前提下做歷史資料治理

`TODOS.md` 裡唯一仍未完成的 `TE9` 就是這件事，因此收斂成獨立 active spec。

## Current State

已知現況：

- 平台有 `audit_logs` 查詢與匯出能力
- 權限模型已區分：
  - `audit_logs.read`
  - `audit_logs.write`
- `audit_logs` 已被當成正式治理功能，而非臨時 debug table
- 目前尚未定義：
  - 保留多久
  - 如何清理
  - 是否歸檔
  - MySQL 單表資料量增長後的處理策略

目前沒有證據顯示現在就必須立刻做 partition，但也沒有證據證明單表無限制成長可以長期接受。

## Goal

定義一套「先可執行、後可演進」的 audit log retention 策略，目標是：

1. 明確定義 audit log 保留期
2. 明確定義清理或歸檔策略
3. 先用最小改動支撐中期成長
4. 若資料量持續成長，再評估 MySQL `PARTITION BY RANGE`

## Non-Goals

本期不做：

- 不立即導入 partition
- 不做獨立 archive storage service
- 不做 Elasticsearch / ClickHouse 類外部稽核查詢系統
- 不重做 audit log schema
- 不把 audit log 改成事件串流平台

原因很簡單：現在要先補的是保留策略，不是提早引入更重的基礎設施。

## Proposed Strategy

### Phase 1：先定義保留期與清理責任

建議先明確定義：

- 預設保留期：`180 天`
- 可接受調整範圍：`180 ~ 365 天`

理由：

- 少於 180 天，對治理追溯可能太短
- 一開始就無限保留，對 MySQL 不夠務實
- 180~365 天足以支撐大多數內部平台的操作稽核需求

### Phase 2：先用應用層定期清理

在還沒有 partition 之前，先採用最小改動策略：

- 新增定期 job
- 依 `created_at` 刪除超過 retention 的資料
- 每次分批刪除，避免長交易與大鎖

設計原則：

- 批次刪除，例如每批 `1,000 ~ 10,000` 筆
- 以時間窗搭配主鍵排序清理
- 每批之間可短暫 sleep，降低對線上查詢影響

### Phase 3：資料量到門檻後再評估 partition

若未來出現以下訊號，再評估 MySQL `PARTITION BY RANGE(created_at)`：

- `audit_logs` 單表資料量已達數百萬級以上
- 刪除 job 對線上負載有明顯影響
- 稽核查詢與匯出延遲顯著增加
- 備份 / restore 時間已不可接受

也就是：

- `partition` 不是現在的預設答案
- `partition` 是當前方案不再足夠時的下一步

## Why Not Partition First

直接上 partition 的問題是：

- migration 與維運複雜度提高
- 需要額外處理 partition 建立、輪替與清理
- 若目前資料量還小，收益不一定大於複雜度

所以務實順序應該是：

```text
先定 retention
  -> 先做批次清理
  -> 觀察資料量與查詢成本
  -> 再決定是否升級到 partition
```

## Implementation Outline

### 1. 補文件與設定來源

至少要有一個明確設定來源，例如：

- 寫死在 config，先用預設值
- 或放進 `platform_settings`

若採可配置方式，建議 key：

- `audit_logs_retention_days`

### 2. 新增 background cleanup job

建議：

- 由 backend 啟動時掛入 scheduler
- 每日執行一次
- 每次循環刪除超過 retention 的舊資料

### 3. 補 audit / monitoring

清理 job 至少應記錄：

- 本次刪除筆數
- 執行耗時
- 失敗原因
- cutoff 時間

避免未來清理 silently fail。

## Acceptance Criteria

1. 專案文件中明確定義 audit log retention 策略。
2. 系統有單一明確的 retention 設定來源。
3. backend 有定期清理超過 retention 的 `audit_logs` 機制。
4. 清理必須採批次方式，避免單次大刪除。
5. 清理 job 會記錄執行結果與失敗資訊。
6. 文件中明確說明：是否升級到 MySQL `PARTITION BY RANGE` 取決於後續資料量與負載觀察，而不是現在直接導入。

## Follow-up Trigger

若出現以下任一條件，應重新開 spec 評估 partition：

- `audit_logs` 行數或容量明顯持續成長
- cleanup job 執行時間持續拉長
- 稽核頁查詢或匯出效能退化
- DBA 明確回報維運成本過高

## Related

- [後端 API 與權限對照](../../reference/backend-api-and-permissions.md)
- [平台 Settings](../../reference/settings.md)
- [Specs 分類總覽](../README.md)
