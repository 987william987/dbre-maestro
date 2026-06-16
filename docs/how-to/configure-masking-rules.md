# How to 設定 Masking Rules

這份指南示範 DBA 如何新增欄位遮罩規則，以及如何用 Unmask Whitelist 處理例外欄位。

## Prerequisites

- 你已登入系統
- 你擁有 `masking_rules.write`
- 你知道要保護的欄位命名規則

## Steps

1. 打開 `Masking Rules` 頁面。

   路徑是 `/masking-rules`。

2. 新增一條 rule，定義欄位匹配方式。

   若只有單一欄位名，用 `exact`；若有命名規律，用 `regex`。

3. 選擇 `mask_mode`。

   常見模式：

   - `full`
   - `partial`
   - `hash`
   - `email`
   - `fixed`
   - `numeric`
   - `datetime`
   - `ip`

4. 填寫 `mask_config` JSON。

   例如手機號碼：

   ```json
   {
     "keep_prefix": 3,
     "keep_suffix": 4,
     "mask_char": "*"
   }
   ```

5. 儲存規則。

   儲存後，新查詢若命中對應欄位，就會依規則遮罩。

6. 若有特殊表欄位不該被遮罩，再新增 Unmask Whitelist。

   例如：

   ```json
   {
     "db_connection_id": 1,
     "database_name": "analytics",
     "table_name": "crm_contacts",
     "column_name": "email"
   }
   ```

7. 到 SQL Editor 驗證結果。

   用實際查詢檢查欄位是否如預期被遮罩，或是否正確被 whitelist 排除。

## Verification

確認以下項目：

- rule 列表中可看到新規則
- whitelist 列表中可看到例外項
- SQL Editor 查詢結果符合遮罩預期
- 多來源 expression 若命中不一致 mode，會退化成 `full`

## Troubleshooting

### 不知道 JSON 要怎麼寫

直接看：

- [Masking 與 DSL 參考](../reference/masking-and-dsl.md)

### 遮罩後星號長度洩漏原始長度

`partial` 目前預設輸出固定長度遮罩。若要明確控制，請設定 `fixed_mask_length`。

### Whitelist 看起來沒生效

先確認：

- 連線、database、table、column 是否完全一致
- SQL 分析是否把輸出欄位追蹤回你以為的來源欄位

## 相關文件

- [Masking 與 DSL](../reference/masking-and-dsl.md)
- [SQL Editor](../reference/sql-editor.md)
