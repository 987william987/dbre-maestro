# DB Metadata

DB Metadata 模組把外部資料庫資產資訊同步成平台內可查的快照，分成 Inventory 與 Objects 兩條線。

## 兩類快照

| 類型 | 資料來源 | 用途 |
|---|---|---|
| Inventory | AWS API | 雲端實例、cluster、engine、endpoint 總覽 |
| Objects | 實際 DB connection metadata | database / schema / table 等物件快照 |

## 頁面入口

| 頁面 | Route | 權限 |
|---|---|---|
| Inventory | `/db-metadata/inventory` | `db_metadata.read` |
| Objects | `/db-metadata/objects` | `db_metadata.read` |

## 展示資料來源

這兩個頁面展示時，查的是平台 Meta DB 中的 snapshot，不是每次打開頁面都去即時掃外部 DB。

這個設計的好處是：

- UI 較穩定
- 避免每次開頁面都直連所有實例
- 可接受掃描失敗與重試，不會直接卡住頁面

## Inventory API

### `GET /api/db-metadata/inventory`

Query parameters：

| 參數 | 型別 | 預設 |
|---|---|---|
| `engine` | `string` | 空 |
| `limit` | `number` | `200` |

回傳會額外計算：

- `mapping_status`
- `mapping_connections`

目前 mapping 主要用 endpoint 與 DB Connections 的 readonly / readwrite host 做比對。

## Object API

### `GET /api/db-metadata/objects`

Query parameters：

| 參數 | 型別 | 預設 |
|---|---|---|
| `db_connection_id` | `number` | 空 |
| `limit` | `number` | `0` |

Object list 在輸出前還會再經過設定過濾：

- `db_metadata_object_enabled`
- `db_metadata_object_enabled_connection_ids`

若 object scan 關閉，或未選任何 connection，頁面結果會是空集合。

## Object Scan 支援資料庫

目前 object snapshot 支援：

- `mysql`
- `postgres`
- `postgresql`

Redis 不會進 object snapshot 清單。

## PostgreSQL 掃描注意事項

PostgreSQL metadata 掃描目前會避開 AWS RDS 的 `rdsadmin` database，因為那是 AWS 系統庫，不適合作為一般 metadata 掃描目標。

## 去重與匹配

目前 inventory 與 objects 是 snapshot 模型，平台以掃描結果入庫後再展示。這代表：

- 前端展示大多來自平台 MySQL 中的 snapshot
- 不會在每次打開頁面時同步直連所有外部 DB
- 掃描失敗會記錄在後端日誌，而不是直接把錯誤灌進 UI 清單

若未來遇到：

- 重複實例
- endpoint 改名
- 實例下線

匹配邏輯應以穩定識別資訊優先，再退回 endpoint / host 比對；但目前實作仍以最小改動為主，主要依 connection host 與 snapshot endpoint 對照。

## 掃描排程

後端啟動時會啟動兩個 background jobs：

- `DBMetadataInventoryJob`
- `DBMetadataObjectJob`

掃描時間由 Settings 的 cron 表達式決定。

## 相關設定

| Key | 用途 |
|---|---|
| `db_metadata_inventory_enabled` | 是否啟用 inventory scan |
| `db_metadata_inventory_regions` | 掃描 region |
| `db_metadata_inventory_engines` | 掃描 engine 類型 |
| `db_metadata_inventory_cron` | inventory scan cron，例如 `0 9 * * *` |
| `db_metadata_object_enabled` | 是否啟用 object scan |
| `db_metadata_object_enabled_connection_ids` | object scan 目標 connection |
| `db_metadata_object_cron` | object scan cron，例如 `0 10 * * *` |
| `db_metadata_cron_timezone` | metadata scan cron 時區 |

## 相關文件

- [平台 Settings](settings.md)
- [DB Connections](db-connections.md)
- [架構總覽](../explanation/architecture-overview.md)
