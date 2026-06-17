import{j as e,g as a}from"./index-ilwv3eT6.js";import{P as d}from"./PageIntro-DIm2m4a-.js";import{A as n}from"./arrow-left-C3h6Zunu.js";const i={full:"{}",partial:`{
  "keep_prefix": 3,
  "keep_suffix": 4,
  "mask_char": "*"
}`,hash:"{}",email:`{
  "keep_local_prefix": 1,
  "keep_domain": true,
  "replacement": "****"
}`,fixed:`{
  "value": "[REDACTED]"
}`,numeric:`{
  "operation": "round",
  "decimals": 0
}`,datetime:`{
  "granularity": "day"
}`,ip:`{
  "keep_segments": 2
}`},l=[{mode:"full",summary:"整個值直接替換成固定 `****`。",notes:["不需要 `mask_config`","適合完全不可展示的欄位"]},{mode:"partial",summary:"保留前後部分內容，中間遮罩。",notes:["必填：`keep_prefix`、`keep_suffix`","可選：`mask_char`、`mask_text`、`fixed_mask_length`","未填 `fixed_mask_length` 時，預設固定輸出 4 個遮罩字元"]},{mode:"hash",summary:"將原值做 HMAC-SHA256，不可逆但可比對是否相同。",notes:["不需要 `mask_config`","適合去識別化關聯"]},{mode:"email",summary:"郵箱專用遮罩。",notes:["常用：`keep_local_prefix`、`keep_domain`、`replacement`"]},{mode:"fixed",summary:"一律替換成固定字串。",notes:["必填：`value`","適合私鑰、助記詞、salt、token"]},{mode:"numeric",summary:"數值取整、四捨五入或歸零。",notes:["`operation`: `round` 或 `zero`","可選：`decimals`"]},{mode:"datetime",summary:"把時間截斷到日或小時。",notes:["`granularity`: `day` 或 `hour`"]},{mode:"ip",summary:"只保留前幾段 IP。",notes:["`keep_segments`: IPv4 常用 `2`"]}],o=[{title:"手機號",pattern:"^(mobile|phone)$",matchType:"regex",maskMode:"partial",config:`{
  "keep_prefix": 3,
  "keep_suffix": 4,
  "mask_char": "*"
}`},{title:"電子郵箱",pattern:"^email$",matchType:"regex",maskMode:"email",config:`{
  "keep_local_prefix": 1,
  "keep_domain": true,
  "replacement": "****"
}`},{title:"錢包地址",pattern:"^(wallet_address|from_addr|to_addr)$",matchType:"regex",maskMode:"partial",config:`{
  "keep_prefix": 4,
  "keep_suffix": 4,
  "mask_text": "......"
}`},{title:"私鑰/助記詞",pattern:"^(private_key|mnemonic|salt)$",matchType:"regex",maskMode:"fixed",config:`{
  "value": "[REDACTED]"
}`},{title:"交易時間",pattern:"^(created_at|tx_time)$",matchType:"regex",maskMode:"datetime",config:`{
  "granularity": "day"
}`},{title:"IP 地址",pattern:"^(ip_address|login_ip)$",matchType:"regex",maskMode:"ip",config:`{
  "keep_segments": 2
}`}];function p(){return e.jsxs("div",{className:"flex min-h-full flex-col gap-3 p-3 sm:p-4",children:[e.jsx(d,{title:"Mask DSL Guide",description:"說明 `column pattern + match type + mask mode + mask config` 的使用方式，供 DBA 在建立規則前查閱。",actions:e.jsxs(a,{to:"/masking-rules",className:"inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-[13px] font-semibold text-ink transition hover:bg-panel-soft",children:[e.jsx(n,{className:"h-4 w-4"}),"Back To Rules"]})}),e.jsxs("section",{className:"rounded-xl border border-border bg-panel shadow-soft",children:[e.jsxs("div",{className:"border-b border-border/80 px-4 py-3",children:[e.jsx("p",{className:"text-[13px] font-semibold text-ink",children:"Basic Structure"}),e.jsx("p",{className:"mt-1 text-[12px] leading-6 text-muted",children:"`column pattern` 決定匹配哪些欄位，`match_type` 決定是精準比對還是 regex，`mask_mode` 決定脫敏方式，`mask_config` 提供對應 mode 的參數。"})]}),e.jsxs("div",{className:"grid gap-3 px-4 py-4 md:grid-cols-2",children:[e.jsxs(t,{title:"match_type",children:[e.jsxs("p",{children:[e.jsx("code",{children:"exact"}),"：欄位名必須完全一致，例如 ",e.jsx("code",{children:"email"})]}),e.jsxs("p",{children:[e.jsx("code",{children:"regex"}),"：用正則一次匹配多個欄位，例如 ",e.jsx("code",{children:"^(mobile|phone)$"})]})]}),e.jsxs(t,{title:"mask_config",children:[e.jsxs("p",{children:["JSON 物件。沒有參數的 mode 可直接填 ",e.jsx("code",{children:"{}"}),"。"]}),e.jsx("p",{children:"有參數的 mode 請參考下方 mode 說明與範例。"})]})]}),e.jsxs("div",{className:"border-t border-border/80 px-4 py-4",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:"完整 Rule Payload 範例"}),e.jsx("p",{className:"mt-1 text-[12px] leading-6 text-muted",children:"在後台建立一條 rule 時，實際上送出的資料結構就是下面這四個欄位："}),e.jsx("pre",{className:"mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink",children:`{
  "column_name": "^(email|contact_email|backup_email)$",
  "match_type": "regex",
  "mask_mode": "email",
  "mask_config": {
    "keep_local_prefix": 1,
    "keep_domain": true,
    "replacement": "****"
  }
}`}),e.jsx("p",{className:"mt-2 text-[12px] leading-6 text-muted",children:"如果你只想精準匹配單一欄位，例如只處理 `ssn`，就把 `match_type` 改成 `exact`，`column_name` 直接填 `ssn`。"})]})]}),e.jsxs("section",{className:"rounded-xl border border-border bg-panel shadow-soft",children:[e.jsx("div",{className:"border-b border-border/80 px-4 py-3",children:e.jsx("p",{className:"text-[13px] font-semibold text-ink",children:"Mask Modes"})}),e.jsx("div",{className:"grid gap-3 px-4 py-4 lg:grid-cols-2",children:l.map(s=>e.jsxs("div",{className:"rounded-xl border border-border bg-panel-soft/60 px-3 py-3",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:e.jsx("code",{children:s.mode})}),e.jsx("p",{className:"mt-1 text-[12px] leading-6 text-muted",children:s.summary}),e.jsx("ul",{className:"mt-2 list-disc pl-5 text-[12px] leading-6 text-muted",children:s.notes.map(r=>e.jsx("li",{children:r},r))}),e.jsx("pre",{className:"mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink",children:i[s.mode]})]},s.mode))})]}),e.jsxs("section",{className:"rounded-xl border border-border bg-panel shadow-soft",children:[e.jsx("div",{className:"border-b border-border/80 px-4 py-3",children:e.jsx("p",{className:"text-[13px] font-semibold text-ink",children:"Common Examples"})}),e.jsx("div",{className:"grid gap-3 px-4 py-4",children:o.map(s=>e.jsxs("div",{className:"rounded-xl border border-border bg-panel-soft/60 px-3 py-3",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:s.title}),e.jsxs("p",{className:"mt-1 text-[11px] leading-5 text-muted",children:["pattern: ",e.jsx("code",{children:s.pattern})," / match: ",e.jsx("code",{children:s.matchType})," / mode: ",e.jsx("code",{children:s.maskMode})]}),e.jsx("pre",{className:"mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink",children:s.config})]},s.title))})]}),e.jsxs("section",{className:"rounded-xl border border-border bg-panel shadow-soft",children:[e.jsxs("div",{className:"border-b border-border/80 px-4 py-3",children:[e.jsx("p",{className:"text-[13px] font-semibold text-ink",children:"How Masking Rules Work With Unmask Whitelist"}),e.jsx("p",{className:"mt-1 text-[12px] leading-6 text-muted",children:"`Masking Rules` 先做廣泛匹配，`Unmask Whitelist` 再對特定 `connection / database / table / column` 做精準豁免。"})]}),e.jsxs("div",{className:"grid gap-3 px-4 py-4",children:[e.jsxs("div",{className:"rounded-xl border border-border bg-panel-soft/60 px-3 py-3",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:"Step 1. 建立一條廣泛規則"}),e.jsx("p",{className:"mt-1 text-[12px] leading-6 text-muted",children:"想把所有 `email` 類欄位都遮罩，可以用 regex 規則一次匹配多個欄位名："}),e.jsx("pre",{className:"mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink",children:`{
  "column_name": "^(email|contact_email|backup_email)$",
  "match_type": "regex",
  "mask_mode": "email",
  "mask_config": {
    "keep_local_prefix": 1,
    "keep_domain": true,
    "replacement": "****"
  }
}`}),e.jsx("p",{className:"mt-2 text-[12px] leading-6 text-muted",children:"這條規則會先命中所有欄位名為 `email`、`contact_email`、`backup_email` 的 MySQL 查詢結果。"})]}),e.jsxs("div",{className:"rounded-xl border border-border bg-panel-soft/60 px-3 py-3",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:"Step 2. 對特定表做 whitelist 豁免"}),e.jsx("p",{className:"mt-1 text-[12px] leading-6 text-muted",children:"如果 `analytics.crm_contacts.email` 是誤傷，不應遮罩，就在 `Unmask Whitelist` 新增一條精準豁免："}),e.jsx("pre",{className:"mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink",children:`{
  "db_connection_id": 1,
  "database_name": "analytics",
  "table_name": "crm_contacts",
  "column_name": "email"
}`})]}),e.jsxs("div",{className:"rounded-xl border border-border bg-panel-soft/60 px-3 py-3",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:"Step 3. 最終效果"}),e.jsxs("div",{className:"mt-2 space-y-2 text-[12px] leading-6 text-muted",children:[e.jsx("p",{children:"`analytics.users.email` ：先命中 regex 規則，沒有 whitelist，結果會被遮罩。"}),e.jsx("p",{children:"`analytics.crm_contacts.email` ：先命中 regex 規則，但又命中 whitelist，結果不遮罩。"}),e.jsx("p",{children:"`analytics.orders.owner_email` ：欄位名不符合 `^(email|contact_email|backup_email)$`，因此不會命中這條規則。"})]})]})]})]}),e.jsxs("section",{className:"rounded-xl border border-border bg-panel shadow-soft",children:[e.jsx("div",{className:"border-b border-border/80 px-4 py-3",children:e.jsx("p",{className:"text-[13px] font-semibold text-ink",children:"Recommended Workflow"})}),e.jsxs("div",{className:"grid gap-3 px-4 py-4 md:grid-cols-3",children:[e.jsxs(t,{title:"1. 先定廣規則",children:[e.jsx("p",{children:"先從欄位命名規則出發，例如 `email`、`phone`、`wallet_address`。"}),e.jsxs("p",{children:["欄位很多時優先用 ",e.jsx("code",{children:"regex"}),"，不要一條一條建。"]})]}),e.jsxs(t,{title:"2. 再挑 mode",children:[e.jsxs("p",{children:["需要可讀性就用 ",e.jsx("code",{children:"partial"})," / ",e.jsx("code",{children:"email"}),"。"]}),e.jsxs("p",{children:["需要完全不可逆就用 ",e.jsx("code",{children:"hash"})," 或 ",e.jsx("code",{children:"fixed"}),"。"]})]}),e.jsxs(t,{title:"3. 最後補 whitelist",children:[e.jsx("p",{children:"若少數表不該遮罩，不要把大規則拆碎。"}),e.jsx("p",{children:"保留廣規則，再用 whitelist 精準豁免特定欄位。"})]})]})]})]})}function t({title:s,children:r}){return e.jsxs("div",{className:"rounded-xl border border-border bg-panel-soft/60 px-3 py-3",children:[e.jsx("p",{className:"text-[12px] font-semibold text-ink",children:s}),e.jsx("div",{className:"mt-2 space-y-1 text-[12px] leading-6 text-muted",children:r})]})}export{p as MaskingDSLGuidePage};
