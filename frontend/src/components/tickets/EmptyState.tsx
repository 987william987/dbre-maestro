import { cn } from '@/lib/utils'

// TD2: 工單佇列空狀態
// 出現時機：DBA 的待辦工單佇列完全清空（pending_review / pending_execution 皆為 0）
// 設計原則：清空佇列是 DBA 達成目標的正向時刻，UI 應給予正面情感反饋，
//           而不只是「沒有資料」的中性提示。

interface EmptyStateProps {
  /** 空狀態的場景類型，決定文案與圖示 */
  variant?: 'queue' | 'history' | 'search'
  onViewHistory?: () => void
  className?: string
}

// Inline SVG: 「勾選完成的工作清單」插畫
// 設計語言：乾淨的線稿 + 品牌色底，與 DESIGN.md 的工具風格一致
function ChecklistIllustration() {
  return (
    <svg
      width="120"
      height="120"
      viewBox="0 0 120 120"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      {/* 底部圓形背景 */}
      <circle cx="60" cy="60" r="56" fill="#ecfdf3" />
      <circle cx="60" cy="60" r="48" fill="#d1fae5" opacity="0.6" />

      {/* 剪貼板主體 */}
      <rect x="34" y="28" width="52" height="64" rx="6" fill="white" stroke="#e7ebf0" strokeWidth="1.5" />

      {/* 剪貼板頂部夾子 */}
      <rect x="48" y="24" width="24" height="10" rx="5" fill="#d1fae5" stroke="#6ee7b7" strokeWidth="1.5" />
      <rect x="54" y="26" width="12" height="6" rx="3" fill="white" />

      {/* 第 1 行：已完成（勾） */}
      <circle cx="47" cy="50" r="5" fill="#12b76a" />
      <polyline points="44,50 46.5,52.5 50,47" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <rect x="56" y="47" width="24" height="2.5" rx="1.25" fill="#e7ebf0" />
      <rect x="56" y="51.5" width="16" height="2" rx="1" fill="#f0f0f0" />

      {/* 第 2 行：已完成（勾） */}
      <circle cx="47" cy="65" r="5" fill="#12b76a" />
      <polyline points="44,65 46.5,67.5 50,62" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <rect x="56" y="62" width="20" height="2.5" rx="1.25" fill="#e7ebf0" />
      <rect x="56" y="66.5" width="28" height="2" rx="1" fill="#f0f0f0" />

      {/* 第 3 行：已完成（勾） */}
      <circle cx="47" cy="80" r="5" fill="#12b76a" />
      <polyline points="44,80 46.5,82.5 50,77" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <rect x="56" y="77" width="26" height="2.5" rx="1.25" fill="#e7ebf0" />

      {/* 右下角裝飾星星 */}
      <circle cx="90" cy="38" r="4" fill="#fef9c3" stroke="#fde047" strokeWidth="1" />
      <circle cx="32" cy="78" r="3" fill="#ede9fe" stroke="#c4b5fd" strokeWidth="1" />
    </svg>
  )
}

function SearchEmptyIllustration() {
  return (
    <svg width="96" height="96" viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <circle cx="48" cy="48" r="44" fill="#f3f4f6" />
      <circle cx="44" cy="42" r="18" stroke="#d0d5dd" strokeWidth="3" fill="white" />
      <line x1="57" y1="56" x2="70" y2="69" stroke="#d0d5dd" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}

function HistoryEmptyIllustration() {
  return (
    <svg width="96" height="96" viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      <circle cx="48" cy="48" r="44" fill="#f3f4f6" />
      <rect x="28" y="28" width="40" height="40" rx="6" fill="white" stroke="#e7ebf0" strokeWidth="1.5"/>
      <rect x="36" y="40" width="24" height="2.5" rx="1.25" fill="#e7ebf0" />
      <rect x="36" y="47" width="16" height="2" rx="1" fill="#f0f0f0" />
      <rect x="36" y="53" width="20" height="2" rx="1" fill="#f0f0f0" />
    </svg>
  )
}

const CONFIG = {
  queue: {
    illustration: <ChecklistIllustration />,
    heading:      '所有工單已處理完畢',
    body:         'DBA 的工作清零，是值得紀念的時刻。繼續保持！',
    action:       '查看歷史工單',
  },
  history: {
    illustration: <HistoryEmptyIllustration />,
    heading:      '尚無歷史工單',
    body:         '工單完成或關閉後將顯示於此。',
    action:       null,
  },
  search: {
    illustration: <SearchEmptyIllustration />,
    heading:      '找不到符合的工單',
    body:         '試著調整篩選條件或清除搜尋關鍵字。',
    action:       null,
  },
} as const

export function EmptyState({ variant = 'queue', onViewHistory, className }: EmptyStateProps) {
  const { illustration, heading, body, action } = CONFIG[variant]

  return (
    <div
      role="status"
      aria-label={heading}
      className={cn(
        'flex flex-col items-center justify-center gap-5 py-16 px-6 text-center',
        className,
      )}
    >
      {illustration}

      <div className="flex flex-col gap-2 max-w-xs">
        <h3 className="text-base font-display font-bold text-ink">{heading}</h3>
        <p className="text-xs text-muted leading-relaxed">{body}</p>
      </div>

      {action && onViewHistory && (
        <button
          onClick={onViewHistory}
          className={cn(
            'mt-1 inline-flex h-9 items-center gap-1.5 rounded-control border border-border',
            'bg-panel px-4 text-xs font-semibold text-ink',
            'hover:bg-page transition-colors',
          )}
        >
          {action}
          <span className="text-faint">›</span>
        </button>
      )}
    </div>
  )
}
