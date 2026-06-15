# Mask DSL Guide

這份文件說明 `Masking Rules` 的 DSL 格式，讓 DBA 可以在不改程式碼的前提下，自行配置哪些欄位要套用哪種脫敏規則。

## Rule 結構

每一條 rule 由四個欄位組成：

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

欄位說明：

- `column_name`: 欄位名稱，或 regex pattern
- `match_type`: `exact` 或 `regex`
- `mask_mode`: 脫敏模式
- `mask_config`: 該 mode 對應的 JSON 參數；沒有參數時可用 `{}` 

## match_type

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

一次匹配多個欄位名，適合有命名規律的場景。

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

## mask_mode

### `full`

整個值直接替換成固定 `****`。

```json
{}
```

### `partial`

保留前後內容，中間遮罩。

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

若未提供 `fixed_mask_length`，系統預設會輸出固定 4 個遮罩字元，避免從遮罩長度反推出原始值長度。

### `hash`

把原值做 HMAC-SHA256，不可逆，但相同原值會得到相同結果。

```json
{}
```

### `email`

郵箱專用遮罩。

```json
{
  "keep_local_prefix": 1,
  "keep_domain": true,
  "replacement": "****"
}
```

### `fixed`

一律替換成固定字串。

```json
{
  "value": "[REDACTED]"
}
```

### `numeric`

數值取整、四捨五入或歸零。

```json
{
  "operation": "round",
  "decimals": 0
}
```

可用參數：

- `operation`: `round` 或 `zero`
- `decimals`

### `datetime`

把時間截斷到日或小時。

```json
{
  "granularity": "day"
}
```

可用參數：

- `granularity`: `day` 或 `hour`

### `ip`

只保留前幾段 IP。

```json
{
  "keep_segments": 2
}
```

## 常見實例

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

### 錢包地址

```json
{
  "column_name": "^(wallet_address|from_addr|to_addr)$",
  "match_type": "regex",
  "mask_mode": "partial",
  "mask_config": {
    "keep_prefix": 4,
    "keep_suffix": 4,
    "mask_text": "......"
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

## 與 Unmask Whitelist 的搭配方式

執行順序是：

1. 先套用 `Masking Rules`
2. 再檢查 `Unmask Whitelist`

也就是說，建議先定義「廣泛規則」，再用 whitelist 精準排除例外。

### Step 1. 建一條廣規則

想把所有 `email` 類欄位都遮罩：

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

### Step 2. 對誤傷欄位加 whitelist

如果 `analytics.crm_contacts.email` 不該遮罩，就新增：

```json
{
  "db_connection_id": 1,
  "database_name": "analytics",
  "table_name": "crm_contacts",
  "column_name": "email"
}
```

### Step 3. 最終效果

- `analytics.users.email`：命中 rule，沒有 whitelist，會被遮罩
- `analytics.crm_contacts.email`：先命中 rule，再命中 whitelist，不遮罩
- `analytics.orders.owner_email`：欄位名不符合 regex，不命中這條 rule

## 建議使用方式

- 欄位很多、命名有規律時，優先用 `regex`
- 少數欄位例外時，不要把大規則拆散，直接加 whitelist
- 需要保留辨識性時，用 `partial` 或 `email`
- 需要完全不可逆時，用 `hash` 或 `fixed`
