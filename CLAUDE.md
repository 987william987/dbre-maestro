# CLAUDE.md

先遵守根目錄 [AGENTS.md](AGENTS.md) 的共用規則與專案文件路由。

## gstack 專屬規則

- 網頁瀏覽一律使用 gstack 的 `/browse` skill，不使用 `mcp__claude-in-chrome__*`。
- 使用者的要求符合目前環境提供的 gstack skill 時，依 `AGENTS.md` 的 skill routing 使用該 skill。
- 可用 skill 清單以目前 session 實際提供的內容為準，不在本檔維護容易失效的固定清單。
