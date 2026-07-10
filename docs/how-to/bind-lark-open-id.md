# How to 綁定使用者 Lark Open ID

這份指南說明管理員如何為平台使用者手動綁定 Lark Open ID，讓工單通知可以正確送到指定人員。

若已啟用 Lark OAuth 登入，使用者用 Lark 登入時系統會自動綁定 `open_id`。手動綁定仍保留，用於 fallback、舊帳號維護與通知問題排查。

## 適用場景

- 使用者尚未透過 Lark OAuth 登入，但已需要收到 Lark 通知
- 某位使用者收不到 Lark 工單通知
- 使用者更換 Lark 帳號，需要更新通知綁定
- OAuth 無法使用，需臨時手動維護通知 recipient

## 重要原則

- `users.email` 只作為平台登入帳號資料，不用來投遞 Lark 訊息
- `users.lark_recipient` 目前保存的是 Lark `open_id`
- 工單通知發送時固定使用 `receive_id_type=open_id`
- 如果 `Lark Open ID` 沒有填，該使用者不會收到定向 Lark 通知
- Lark OAuth 自動綁定時，平台只使用 Lark `enterprise_email` 匹配或建立 user，不使用 personal email

## 前置條件

- 你有 `users.write` 權限
- 平台 `Settings` 已配置 `Lark App ID` 與 `Lark App Secret`
- 你已拿到該使用者可投遞的 Lark `open_id`

## 操作步驟

1. 打開 `User Management` 頁面。

   路徑是 `/users`。

2. 在 `Users` 分頁中找到目標使用者，點擊 `Manage`。

3. 在右側抽屜的 `Lark Open ID` 欄位填入該使用者的 `open_id`。

   範例格式：

   ```text
   ou_xxxxxxxxxxxxx
   ```

4. 點擊 `Save Changes`。

   系統會先顯示確認摘要，再正式寫入。

## 驗證方式

綁定完成後，可以用以下方式確認：

- 在 `User Management` 列表中看到該使用者已顯示 `Lark Open ID`
- 由該使用者參與一次工單流程
- 確認 submitter、reviewer 或 executor 能即時收到對應的 Lark 訊息

## 故障排查

### 已填 `Lark Open ID`，但還是收不到通知

請依序檢查：

- `Lark Open ID` 是否填錯或填到別人的 `open_id`
- 平台 `Settings` 中的 `Lark App ID` / `Lark App Secret` 是否正確
- 該 Lark app 是否有發送訊息所需權限
- 該使用者是否在 Lark app 可觸達的範圍內

### 可以收到站內信，但收不到 Lark

這代表工單通知事件本身有觸發，但 Lark 投遞鏈路有問題。優先檢查：

- `Lark Open ID` 是否正確
- Lark app 權限與資料範圍
- 後端 `notification_failure` audit log 與 app log

### OAuth 登入後仍收不到 Lark 通知

請確認該 user profile 是否已寫入 `Lark Open ID`。若沒有，可能是 OAuth callback 未取得 `open_id`，或登入流程未完成。可重新登入一次，或由 admin 手動填入可投遞的 `open_id`。

## 維運建議

- 優先讓一般使用者透過 Lark OAuth 首次登入，讓平台自動建立或綁定 `open_id`
- 對需要在首次登入前就收到通知的既有 user，再手動維護 Lark Open ID
- 若通知失敗，先查 user profile 的 `lark_recipient`，再查 Lark app 權限與 `notification_failure` audit log
