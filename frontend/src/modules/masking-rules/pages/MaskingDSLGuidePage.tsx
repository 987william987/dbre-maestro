const MASK_MODE_EXAMPLES = {
  full: '{}',
  partial: '{\n  "keep_prefix": 3,\n  "keep_suffix": 4,\n  "mask_char": "*"\n}',
  hash: '{}',
  email: '{\n  "keep_local_prefix": 1,\n  "keep_domain": true,\n  "replacement": "****"\n}',
  fixed: '{\n  "value": "[REDACTED]"\n}',
  numeric: '{\n  "operation": "round",\n  "decimals": 0\n}',
  datetime: '{\n  "granularity": "day"\n}',
  ip: '{\n  "keep_segments": 2\n}',
} as const

const MODE_GUIDES = [
  {
    mode: 'full',
    summary: '整個值直接替換成固定 `****`。',
    notes: ['不需要 `mask_config`', '適合完全不可展示的欄位'],
  },
  {
    mode: 'partial',
    summary: '保留前後部分內容，中間遮罩。',
    notes: ['必填：`keep_prefix`、`keep_suffix`', '可選：`mask_char`、`mask_text`、`fixed_mask_length`', '未填 `fixed_mask_length` 時，預設固定輸出 4 個遮罩字元'],
  },
  {
    mode: 'hash',
    summary: '將原值做 HMAC-SHA256，不可逆但可比對是否相同。',
    notes: ['不需要 `mask_config`', '適合去識別化關聯'],
  },
  {
    mode: 'email',
    summary: '郵箱專用遮罩。',
    notes: ['常用：`keep_local_prefix`、`keep_domain`、`replacement`'],
  },
  {
    mode: 'fixed',
    summary: '一律替換成固定字串。',
    notes: ['必填：`value`', '適合私鑰、助記詞、salt、token'],
  },
  {
    mode: 'numeric',
    summary: '數值取整、四捨五入或歸零。',
    notes: ['`operation`: `round` 或 `zero`', '可選：`decimals`'],
  },
  {
    mode: 'datetime',
    summary: '把時間截斷到日或小時。',
    notes: ['`granularity`: `day` 或 `hour`'],
  },
  {
    mode: 'ip',
    summary: '只保留前幾段 IP。',
    notes: ['`keep_segments`: IPv4 常用 `2`'],
  },
] as const

const RULE_EXAMPLES = [
  {
    title: '手機號',
    pattern: '^(mobile|phone)$',
    matchType: 'regex',
    maskMode: 'partial',
    config: '{\n  "keep_prefix": 3,\n  "keep_suffix": 4,\n  "mask_char": "*"\n}',
  },
  {
    title: '電子郵箱',
    pattern: '^email$',
    matchType: 'regex',
    maskMode: 'email',
    config: '{\n  "keep_local_prefix": 1,\n  "keep_domain": true,\n  "replacement": "****"\n}',
  },
  {
    title: '錢包地址',
    pattern: '^(wallet_address|from_addr|to_addr)$',
    matchType: 'regex',
    maskMode: 'partial',
    config: '{\n  "keep_prefix": 4,\n  "keep_suffix": 4,\n  "mask_text": "......"\n}',
  },
  {
    title: '私鑰/助記詞',
    pattern: '^(private_key|mnemonic|salt)$',
    matchType: 'regex',
    maskMode: 'fixed',
    config: '{\n  "value": "[REDACTED]"\n}',
  },
  {
    title: '交易時間',
    pattern: '^(created_at|tx_time)$',
    matchType: 'regex',
    maskMode: 'datetime',
    config: '{\n  "granularity": "day"\n}',
  },
  {
    title: 'IP 地址',
    pattern: '^(ip_address|login_ip)$',
    matchType: 'regex',
    maskMode: 'ip',
    config: '{\n  "keep_segments": 2\n}',
  },
] as const

export function MaskingDSLGuidePage() {
  return (
    <div className="flex min-h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4 py-3">
          <p className="text-[13px] font-semibold text-ink">Basic Structure</p>
          <p className="mt-1 text-[12px] leading-6 text-muted">
            `column pattern` 決定匹配哪些欄位，`match_type` 決定是精準比對還是 regex，`mask_mode` 決定脫敏方式，`mask_config`
            提供對應 mode 的參數。
          </p>
        </div>
        <div className="grid gap-3 px-4 py-4 md:grid-cols-2">
          <GuideCard title="match_type">
            <p><code>exact</code>：欄位名必須完全一致，例如 <code>email</code></p>
            <p><code>regex</code>：用正則一次匹配多個欄位，例如 <code>^(mobile|phone)$</code></p>
          </GuideCard>
          <GuideCard title="mask_config">
            <p>JSON 物件。沒有參數的 mode 可直接填 <code>{'{}'}</code>。</p>
            <p>有參數的 mode 請參考下方 mode 說明與範例。</p>
          </GuideCard>
        </div>
        <div className="border-t border-border/80 px-4 py-4">
          <p className="text-[12px] font-semibold text-ink">完整 Rule Payload 範例</p>
          <p className="mt-1 text-[12px] leading-6 text-muted">
            在後台建立一條 rule 時，實際上送出的資料結構就是下面這四個欄位：
          </p>
          <pre className="mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink">
{`{
  "column_name": "^(email|contact_email|backup_email)$",
  "match_type": "regex",
  "mask_mode": "email",
  "mask_config": {
    "keep_local_prefix": 1,
    "keep_domain": true,
    "replacement": "****"
  }
}`}
          </pre>
          <p className="mt-2 text-[12px] leading-6 text-muted">
            如果你只想精準匹配單一欄位，例如只處理 `ssn`，就把 `match_type` 改成 `exact`，`column_name` 直接填 `ssn`。
          </p>
        </div>
      </section>

      <section className="rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4 py-3">
          <p className="text-[13px] font-semibold text-ink">Mask Modes</p>
        </div>
        <div className="grid gap-3 px-4 py-4 lg:grid-cols-2">
          {MODE_GUIDES.map((item) => (
            <div key={item.mode} className="rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
              <p className="text-[12px] font-semibold text-ink"><code>{item.mode}</code></p>
              <p className="mt-1 text-[12px] leading-6 text-muted">{item.summary}</p>
              <ul className="mt-2 list-disc pl-5 text-[12px] leading-6 text-muted">
                {item.notes.map((note) => (
                  <li key={note}>{note}</li>
                ))}
              </ul>
              <pre className="mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink">
                {MASK_MODE_EXAMPLES[item.mode]}
              </pre>
            </div>
          ))}
        </div>
      </section>

      <section className="rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4 py-3">
          <p className="text-[13px] font-semibold text-ink">Common Examples</p>
        </div>
        <div className="grid gap-3 px-4 py-4">
          {RULE_EXAMPLES.map((example) => (
            <div key={example.title} className="rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
              <p className="text-[12px] font-semibold text-ink">{example.title}</p>
              <p className="mt-1 text-[11px] leading-5 text-muted">
                pattern: <code>{example.pattern}</code> / match: <code>{example.matchType}</code> / mode: <code>{example.maskMode}</code>
              </p>
              <pre className="mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink">
                {example.config}
              </pre>
            </div>
          ))}
        </div>
      </section>

      <section className="rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4 py-3">
          <p className="text-[13px] font-semibold text-ink">How Masking Rules Work With Unmask Whitelist</p>
          <p className="mt-1 text-[12px] leading-6 text-muted">
            `Masking Rules` 先做廣泛匹配，`Unmask Whitelist` 再對特定 `connection / database / table / column` 做精準豁免。
          </p>
        </div>
        <div className="grid gap-3 px-4 py-4">
          <div className="rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
            <p className="text-[12px] font-semibold text-ink">Step 1. 建立一條廣泛規則</p>
            <p className="mt-1 text-[12px] leading-6 text-muted">
              想把所有 `email` 類欄位都遮罩，可以用 regex 規則一次匹配多個欄位名：
            </p>
            <pre className="mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink">
{`{
  "column_name": "^(email|contact_email|backup_email)$",
  "match_type": "regex",
  "mask_mode": "email",
  "mask_config": {
    "keep_local_prefix": 1,
    "keep_domain": true,
    "replacement": "****"
  }
}`}
            </pre>
            <p className="mt-2 text-[12px] leading-6 text-muted">
              這條規則會先命中所有欄位名為 `email`、`contact_email`、`backup_email` 的 MySQL / PostgreSQL 查詢結果。
            </p>
          </div>

          <div className="rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
            <p className="text-[12px] font-semibold text-ink">Step 2. 對特定表做 whitelist 豁免</p>
            <p className="mt-1 text-[12px] leading-6 text-muted">
              如果 `analytics.crm_contacts.email` 是誤傷，不應遮罩，就在 `Unmask Whitelist` 新增一條精準豁免：
            </p>
            <pre className="mt-3 overflow-x-auto rounded-md border border-border bg-white px-3 py-2 font-mono text-[11px] leading-5 text-ink">
{`{
  "db_connection_id": 1,
  "database_name": "analytics",
  "table_name": "crm_contacts",
  "column_name": "email"
}`}
            </pre>
          </div>

          <div className="rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
            <p className="text-[12px] font-semibold text-ink">Step 3. 最終效果</p>
            <div className="mt-2 space-y-2 text-[12px] leading-6 text-muted">
              <p>
                `analytics.users.email`
                ：先命中 regex 規則，沒有 whitelist，結果會被遮罩。
              </p>
              <p>
                `analytics.crm_contacts.email`
                ：先命中 regex 規則，但又命中 whitelist，結果不遮罩。
              </p>
              <p>
                `analytics.orders.owner_email`
                ：欄位名不符合 `^(email|contact_email|backup_email)$`，因此不會命中這條規則。
              </p>
            </div>
          </div>
        </div>
      </section>

      <section className="rounded-xl border border-border bg-panel shadow-soft">
        <div className="border-b border-border/80 px-4 py-3">
          <p className="text-[13px] font-semibold text-ink">Recommended Workflow</p>
        </div>
        <div className="grid gap-3 px-4 py-4 md:grid-cols-3">
          <GuideCard title="1. 先定廣規則">
            <p>先從欄位命名規則出發，例如 `email`、`phone`、`wallet_address`。</p>
            <p>欄位很多時優先用 <code>regex</code>，不要一條一條建。</p>
          </GuideCard>
          <GuideCard title="2. 再挑 mode">
            <p>需要可讀性就用 <code>partial</code> / <code>email</code>。</p>
            <p>需要完全不可逆就用 <code>hash</code> 或 <code>fixed</code>。</p>
          </GuideCard>
          <GuideCard title="3. 最後補 whitelist">
            <p>若少數表不該遮罩，不要把大規則拆碎。</p>
            <p>保留廣規則，再用 whitelist 精準豁免特定欄位。</p>
          </GuideCard>
        </div>
      </section>
    </div>
  )
}

function GuideCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-border bg-panel-soft/60 px-3 py-3">
      <p className="text-[12px] font-semibold text-ink">{title}</p>
      <div className="mt-2 space-y-1 text-[12px] leading-6 text-muted">{children}</div>
    </div>
  )
}
