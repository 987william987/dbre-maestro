# 驗證 Online DDL Execution

本手冊用於驗證 MySQL／Aurora MySQL DDL 工單的 Native、gh-ost 與 pt-osc 執行模式。功能開關預設關閉；未完成 Testnet 驗收前，不應在 Production 啟用外部工具模式。

## 本機真實工具驗收

```bash
make test-online-ddl-integration
```

測試會建立隔離的 MySQL 8 source 與 async replica，啟用 ROW binlog、FULL row image 與 GTID，並使用 application image 內固定版本的工具。測試結束後會移除 containers、network 與 volumes。

目前涵蓋兩個工具的 version、replica topology、真實 cut-over、小表與 65,536-row copy、copy 期間 concurrent DML、Pause／Resume／Cancel、取消後 source table 與 artifacts，以及錯誤 credential／endpoint 的 bounded failure。其他 package tests 驗證 dry-run argv、tool panic、server shutdown、restart interruption、timeout、process-group cleanup、bounded output、OCC、artifact verification 與 outcome classification。

## Aurora Testnet 驗收

每次驗收需保存以下資料；沒有可追溯紀錄時不得視為通過：

```text
環境／cluster：
Aurora engine version：
writer／reader topology：
application image tag／commit：
gh-ost／pt-osc version：
操作者：
開始／結束時間（含 timezone）：
測試 ticket refs：
結果與 artifacts：
CloudWatch／application log reference：
```

1. 確認兩個 mode 關閉，Native、manual、batch、scheduled 與 auto-after-approval regression 正常。
2. 只開啟 gh-ost，在專用 schema 驗證 dry-run、小表、較大表、concurrent DML、replica lag throttle、metadata lock、Pause／Resume／Cancel 與 credential rotation failure。
3. 關閉 gh-ost，確認 active run 不被中止且新的 gh-ost Execute 被拒絕。
4. 只開啟 pt-osc，重複前述案例，另確認工具取得可連線的 registered replica Host/Port。
5. 驗證 app shutdown/restart 將 active run 保存為 `interrupted`，只盤點 artifacts，不自動 resume 或 DROP。
6. 驗證 pod ephemeral-storage、memory、PID 與 execution timeout 限制；超限時 source table 保留，終態與 artifacts 可追溯。
7. 關閉兩個 mode，再跑 Native 與通知 regression；第一版 Lark 卡片維持既有簡單工單狀態。

任何 `outcome_unknown`、殘留 artifacts、replica discovery 失敗或 topology 不符都必須停止 rollout，由 DBA 人工確認；平台不自動清理。
