# Masking 與 DSL

本文件整理欄位脫敏規則、Unmask Whitelist 與 SQL 查詢結果的套用方式。

## 目標

Masking 系統的目標是讓 DBA 透過後台配置：

- 哪些欄位屬於敏感欄位
- 命中後要套用哪一種 mask mode
- 哪些表欄位是例外，可從全域規則中排除

這個模型是「欄位 + 規則」導向，不需要為每個欄位把程式碼寫死。

## Rule 結構

每條 rule 包含四個欄位：

```json
{
  "column_name": "^(email|contact_email|backup_email)$",
  "match_type": "regex",
  "mask_mode": "email",
  "mask_config": {
    "keep_local_prefix": 1,
    "keep_domain": true,
    "replacement": "****"
  }
}
```

| 欄位 | 型別 | 說明 |
|---|---|---|
| `column_name` | `string` | 欄位名或 regex pattern |
| `match_type` | `exact` 或 `regex` | 匹配方式 |
| `mask_mode` | `string` | 遮罩模式 |
| `mask_config` | `object` | mode 專屬設定 |

## Match Type

### `exact`

只匹配單一欄位名。

```json
{
  "column_name": "ssn",
  "match_type": "exact",
  "mask_mode": "full",
  "mask_config": {}
}
```

### `regex`

一次匹配多個命名規律相近的欄位。

```json
{
  "column_name": "^(mobile|phone|contact_phone)$",
  "match_type": "regex",
  "mask_mode": "partial",
  "mask_config": {
    "keep_prefix": 3,
    "keep_suffix": 4,
    "mask_char": "*"
  }
}
```

## Mask Mode

### `full`

整個值以固定遮罩取代。

```json
{}
```

### `partial`

保留前後字元，中間以固定長度遮罩。

```json
{
  "keep_prefix": 3,
  "keep_suffix": 4,
  "mask_char": "*"
}
```

可用參數：

- `keep_prefix`
- `keep_suffix`
- `mask_char`
- `mask_text`
- `fixed_mask_length`

若未提供 `fixed_mask_length`，系統預設輸出固定 4 個遮罩字元，避免從星號長度反推原始值長度。

### `hash`

以 HMAC-SHA256 做不可逆轉換。

```json
{}
```

### `email`

信箱專用遮罩。

```json
{
  "keep_local_prefix": 1,
  "keep_domain": true,
  "replacement": "****"
}
```

### `fixed`

固定替換值。

```json
{
  "value": "[REDACTED]"
}
```

### `numeric`

數值遮罩。

```json
{
  "operation": "round",
  "decimals": 0
}
```

支援：

- `operation`: `round` 或 `zero`
- `decimals`

### `datetime`

時間截斷。

```json
{
  "granularity": "day"
}
```

支援：

- `granularity`: `day` 或 `hour`

### `ip`

只保留前幾段 IP。

```json
{
  "keep_segments": 2
}
```

## Unmask Whitelist

Whitelist 用來排除「本來會被廣規則命中，但某個實際表欄位不該遮罩」的情況。

結構：

```json
{
  "db_connection_id": 1,
  "database_name": "analytics",
  "table_name": "crm_contacts",
  "column_name": "email"
}
```

## 套用順序

執行順序是：

1. 先命中 Masking Rule
2. 再檢查 Unmask Whitelist

也就是說，Whitelist 仍然有效；它不是被 DSL 替代，而是跟 DSL 搭配使用。

## SQL 分析與來源追蹤

系統不只看輸出欄位名稱，還會分析 SQL expression 與欄位來源。

目前重點能力包括：

- `SELECT *` 展開
- alias 追蹤
- join 欄位來源追蹤
- CTE / subquery 的部分 lineage 分析
- 多來源 expression 判斷

## 多來源 expression 的遮罩規則

若一個輸出欄位依賴多個敏感來源：

- 若命中 2 個以上敏感欄位，且 rule mode 不一致，直接退化成 `full`
- 若命中的敏感來源只有一種 mode，沿用該 mode

這是為了避免把多來源混合欄位以過度樂觀的方式部分顯示。

## 常見範例

### 手機號

```json
{
  "column_name": "^(mobile|phone)$",
  "match_type": "regex",
  "mask_mode": "partial",
  "mask_config": {
    "keep_prefix": 3,
    "keep_suffix": 4,
    "mask_char": "*"
  }
}
```

### 電子郵箱

```json
{
  "column_name": "^email$",
  "match_type": "regex",
  "mask_mode": "email",
  "mask_config": {
    "keep_local_prefix": 1,
    "keep_domain": true,
    "replacement": "****"
  }
}
```

### 私鑰 / 助記詞

```json
{
  "column_name": "^(private_key|mnemonic|salt)$",
  "match_type": "regex",
  "mask_mode": "fixed",
  "mask_config": {
    "value": "[REDACTED]"
  }
}
```

## 相關頁面

- `Masking Rules`：管理 rules 與 whitelist
- `SQL Editor`：查詢結果遮罩

## 相關文件

- [How to 設定 Masking Rules](../how-to/configure-masking-rules.md)
- [SQL Editor](sql-editor.md)
