# DESIGN.md

DBRE Maestro 目前已批准的設計方向，來源於 `sql-editor` finalized 稿。
這份文件是後續 `/design-shotgun`、`/design-html` 與實作頁面的共同基準。

## 設計定位

- 產品類型：內部 DBA / 工程團隊使用的資料庫治理工作台
- 視覺語氣：乾淨、冷靜、偏專業工具，不走行銷感或展示感
- 互動原則：主操作前景化，治理資訊近身但不搶焦點
- 版面原則：優先保障主工作區面積，避免為提示或摘要切碎畫面

## 色彩 Tokens

```css
:root {
  --page: #f6f7fb;
  --panel: #ffffff;
  --panel-muted: #fbfcfe;
  --panel-soft: #f7f9fc;
  --sidebar: #ffffff;
  --editor: #f9fbfd;
  --editor-border: #dbe3ec;
  --editor-toolbar: #f3f6fa;
  --editor-code-bg: #eef3f8;
  --editor-code-text: #1f2937;
  --text: #101828;
  --text-secondary: #667085;
  --text-muted: #98a2b3;
  --brand: #111827;
  --accent: #2563eb;
  --accent-soft: #eff6ff;
  --success: #12b76a;
  --warning: #f79009;
  --danger: #f04438;
  --border: #e7ebf0;
  --border-strong: #d0d5dd;
}
```

## 字體系統

- UI Sans：`Manrope`, `Noto Sans TC`, `sans-serif`
- Display / Page Title：`Sora`, `Manrope`, `sans-serif`
- Mono / SQL / IDs：`JetBrains Mono`, `monospace`

## 字重與層級

- Page title：`Sora 800`, 28px, 緊字距
- Section title：`Sora/Manrope 700-800`, 18px
- Navigation / primary button：`Manrope 700`
- Body：`Manrope 400`, 13px / 1.5
- Meta / secondary copy：11-12px，使用 `--text-secondary` 或 `--text-muted`

## 間距與尺寸

- 外層 page gutter：`18px`
- Sidebar width：`252px`
- Card padding：`18px`
- 小型 control 高度：`36px`
- Search / input 高度：`40px`
- Card radius：`18px`
- Control radius：`12px`
- Pill radius：`999px`

## 陰影與邊框

```css
--shadow: 0 18px 38px rgba(16, 24, 40, 0.06);
--shadow-soft: 0 4px 14px rgba(16, 24, 40, 0.05);
```

- 預設邊框色：`--border`
- 強調邊框：`--border-strong`
- 不使用厚重投影或高飽和外光暈

## 版面模式

- 頁面基底：左側固定 sidebar + 右側主工作區
- 主工作區：先是 page heading，再是工作模組
- 內容頁優先使用「一大主區 + 次工具列」結構
- 對 SQL Editor 類頁面：
  - 第一屏優先保留 tab、editor、主要操作
  - result table 應盡量跨滿可用寬度
  - 治理規則應使用收合、pill、popover 或近身提示，不應形成大塊獨立模組

## 元件語氣

### Sidebar
- 白底、輕微玻璃感
- active item 用淡灰底，不用高彩色塊
- nav label 用 11px 大寫輔助分組

### Tabs
- 預設白底描邊
- active tab 深色底、白字
- 不做誇張陰影或漸層

### Buttons
- Primary：深色實底 `#111827 -> #1f2937`
- Secondary / ghost：白底、細邊框
- 高頻次要操作可放 toolbar，但要靠排序而不是色彩搶焦點

### Chips / Pills
- 用於 limit、status、guardrails 這類輕量上下文
- 預設白底邊框
- 成功 / 警告 / 錯誤狀態才用有色底

### Cards
- 白底、細邊框、輕陰影
- 不使用厚重彩色背景卡作為預設

### Table
- 表頭使用淡灰背景
- 敏感欄位表頭可用 danger 色提示
- 表格優先資訊密度，不做花俏 row decoration

## 互動準則

- 使用者最常做的事必須永遠在第一視野
- 額外治理資訊預設收合或弱化，但必須可快速展開
- Search 不是每頁必備；若沒有明確查找需求，可以省略
- Follow-up actions 盡量併回 result toolbar 或主工作區，不另外拆一張 summary card
- 空間是稀缺資源，不要為了「看起來完整」塞入大面積提示模組

## 響應式準則

- Desktop：維持 sidebar + content 雙欄
- Tablet：主內容可降成單欄，但保留原本資訊群組順序
- Mobile：優先保住標題、主要操作、資料結果；次要提示改收合

## 不該出現的風格

- 預設紫藍漸層 hero
- 行銷頁式大標語 + 大插畫
- 過度圓潤的 SaaS landing page 卡片感
- 為了提示而新增大面積獨立模組
- 顏色過多的 status 系統
- 把內部工具做成像品牌官網

## 後續頁面對齊要求

後續若做 `SQL Requests`、`Dashboard`、`Permission Requests`、`Audit Log`：

- 延續同一套字體、色票、radius、shadow、border
- 延續「高密度但不髒亂」的版面節奏
- 治理資訊一律當作近身輔助，不可喧賓奪主
- 若新頁面需要例外風格，必須明確說明為什麼這頁要偏離
