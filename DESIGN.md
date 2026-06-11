# DESIGN.md — Admin Dashboard 設計規範

> 參考來源：[Shadcnblocks Admin Kit](https://www.shadcnblocks.com/admin-dashboard)（[Live Demo](https://shadcnblocks-admin.vercel.app/)）
> 技術基礎：Next.js (App Router) + shadcn/ui + Tailwind CSS + TypeScript + Recharts

---

## 1. 整體風格

- **極簡單色（Monochrome）**：以黑、白、灰為主色調，圖表也以深淺灰階呈現，僅在語意處使用彩色。
- **卡片式版面**：淺灰頁面背景（`bg-muted/40`）+ 白色卡片，圓角 `rounded-xl`，細邊框 `border`，無陰影或極淡陰影。
- **大量留白**：卡片內 padding 充足（`p-6`），區塊間距 `gap-4`～`gap-6`。
- **語意色僅兩種**：綠色 = 正向趨勢（+27.9%），紅色 = 負向趨勢（-13.7%）與通知 badge。
- **支援明暗模式與主題色切換**：CSS variables 驅動（shadcn theming），頂部欄提供 light/dark toggle 與 theme preset 選擇器。

## 2. 版面結構（App Shell）

```
┌──────────┬──────────────────────────────────────────────┐
│          │ Topbar：☰ | App 名稱      🔍⌘K 🔔 ☀️ 主題選擇 │
│ Sidebar  ├──────────────────────────────────────────────┤
│ (256px,  │ Breadcrumb：Kit / Products / Product List    │
│ 可收合)  ├──────────────────────────────────────────────┤
│          │ 主內容區（獨立捲動容器、max-width 置中）       │
│ 群組導覽 │   PageHeader + 卡片網格 / 表格 / 表單          │
│ 底部用戶 │                                              │
└──────────┴──────────────────────────────────────────────┘
```

### 2.1 Sidebar（左側欄）
- 寬約 256px，可收合為 icon-only 模式（shadcn `Sidebar` 元件，`collapsible="icon"`）。
- **頂部**：workspace/team switcher（logo + 名稱 + 副標 + 上下箭頭）。
- **中段**：依領域分群（如 `Ecommerce`、`Project Management`、`Manage`），群組標籤為小型 muted 文字。
- 導覽項目：icon + 文字 + 右側 chevron（可展開子選單）；子選單以左側豎線縮排呈現；當前頁高亮（淺灰底）。
- **底部**：使用者卡片（avatar + 名稱 + email + 選單）。

### 2.2 Topbar（頂部欄）
- 左：sidebar 收合鈕 + 目前 App 名稱。
- 右：全域搜尋（觸發 `⌘K` Command Palette）、通知鈴鐺（紅色數字 badge）、明暗切換、主題色選擇器（色點 + 名稱下拉）。

### 2.3 第二層工具列
- Breadcrumb（目前路徑）+ 頁面內搜尋框（"Search pages or run commands"）。

## 3. 頁面模式（Page Patterns）

### 3.1 Dashboard（總覽頁）
1. **KPI Stat Row**：4 格等寬統計卡（以分隔線相隔、共用一張卡片）。每格：icon + 指標名稱（muted）→ 前期數值（小字 muted）→ **大號粗體數值**（text-3xl/4xl font-bold）→ 趨勢箭頭 + 百分比（綠/紅）+ "vs last month"。
2. **主圖表列**：2 欄（約 5:4）。卡片標頭 = 方形 icon 容器 + 標題 + 右側 legend；折線/面積圖（本年 vs 去年，黑線 + 灰面積）、水平堆疊長條圖（各通路營收，灰階區分）。
3. **次要卡片列**：3 欄等寬。迷你長條圖、雙線比較圖、donut 圖（中心顯示最大占比 + 右側 legend 列表附百分比）。
4. 圖表使用 Recharts（shadcn `Chart` 包裝），一律灰階配色、淺色格線、無多餘裝飾。

### 3.2 列表頁（Data Table）
- **PageHeader**：大標題（text-3xl/4xl font-bold）+ 一行 muted 描述；右側主要 CTA 為**黑底白字按鈕**（icon + 文字，如 "+ Add Products"）。
- **Tabs**：底線式 tab（如 "All products"）。
- **篩選列**：左側搜尋框 + 多個下拉篩選（Category / Supplier / Stock level）；右側 "Manage Table"（欄位顯示設定）。
- **表格**：
  - 表頭淺灰底、欄位可排序（名稱 + 排序箭頭）。
  - 儲存格模式：縮圖 + 名稱（粗體）、等寬碼（SKU）、灰底 `Badge`（分類）、複合資訊（庫存數 + High/Low 標籤 + 綠色進度條）、金額靠右。
  - 每列尾端 `⋯` DropdownMenu（row actions）。
- 表格下方：分頁器 + 筆數資訊。

### 3.3 表單頁（Create / Edit）
- **PageHeader**：標題 + 描述；右側動作列：`Reset`（ghost）、`Save draft`（outline）、`Save Product`（黑色 primary）。
- **兩欄布局**（約 2:1）：
  - 主欄：表單卡片分區（如 "Product Info"），每區有標題 + muted 說明。
  - 側欄：次要設定卡片（如 "Pricing"），sticky 跟隨捲動。
- 欄位規範：label 在上方、placeholder 給範例值（"Shirt, t-shirts, etc."）、欄位下方輔助說明用 muted 小字；相關欄位並排（SKU / Barcode）；金額欄位前綴幣別。
- 圖片上傳：虛線框預覽區 + "Upload image" / "Remove" 文字按鈕 + 規格說明。

## 4. 設計 Tokens

| Token | 值（建議） |
|---|---|
| 字體 | Geist / Inter（sans-serif），等寬碼用 mono |
| 頁面背景 | `--background`（近白）/ 內容區 `bg-muted/40` |
| 卡片 | `bg-card` 白、`border`、`rounded-xl` |
| 主要按鈕 | 黑底白字（`bg-primary text-primary-foreground`） |
| 文字層級 | 標題 `font-bold`；說明/標籤 `text-muted-foreground text-sm` |
| 正向/負向 | green-600 / red-600（僅用於趨勢與警示） |
| 圓角 | 卡片 `xl`、按鈕/輸入框 `md`、badge `full` |
| 間距 | 卡片內 `p-6`，網格 `gap-4`～`gap-6` |

## 5. 元件對照（shadcn/ui）

| 用途 | 元件 |
|---|---|
| App Shell | `Sidebar`（collapsible icon）、`Breadcrumb` |
| 全域搜尋 | `Command`（⌘K palette） |
| 統計/圖表 | `Card` + `Chart`（Recharts：Area/Bar/Line/Pie） |
| 列表 | `Table`、`Tabs`、`Badge`、`Progress`、`DropdownMenu`、`Pagination`、`Select`、`Input` |
| 表單 | `Form`（react-hook-form + zod）、`Input`、`Select`、`Textarea`、`Button` |
| 其他 | `Avatar`、`Tooltip`、`Sheet`（行動版 sidebar）、`Sonner`（toast） |

## 6. 互動與響應式

- `⌘K` 開啟 Command Palette（頁面導覽 + 指令）。
- Sidebar 可收合；行動版改用 `Sheet` 抽屜。
- 主內容區為獨立捲動容器（topbar / sidebar 固定）。
- 表格在窄螢幕允許橫向捲動或隱藏次要欄位（搭配 Manage Table）。
- 響應式斷點：KPI 4 欄 → 2 欄 → 1 欄；圖表列 2 欄 → 1 欄；表單兩欄 → 單欄。
