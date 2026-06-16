import type { ReactNode } from 'react'

type FeaturePlaceholderProps = {
  eyebrow: string
  title: string
  description: string
  aside?: ReactNode
}

export function FeaturePlaceholder({ eyebrow, title, description, aside }: FeaturePlaceholderProps) {
  return (
    <div className="flex h-full flex-col gap-6 p-5 sm:p-6">
      <div className="flex flex-col gap-2 border-b border-border pb-5">
        <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">{eyebrow}</p>
        <h2 className="font-display text-2xl font-black tracking-tight text-ink">{title}</h2>
        <p className="max-w-3xl text-sm text-muted">{description}</p>
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.2fr_0.8fr]">
        <section className="rounded-card border border-border bg-panel-soft p-5">
          <p className="text-sm font-semibold text-ink">This page scaffold and entry point are already in place</p>
          <p className="mt-2 text-xs text-muted">
            The next phase will add data loading, forms, permission controls, and error handling based on `FRONTEND_SPEC.md`.
          </p>
        </section>
        <section className="rounded-card border border-border bg-panel p-5">
          {aside ?? (
            <>
              <p className="text-[11px] font-bold uppercase tracking-[0.2em] text-faint">Next Step</p>
              <p className="mt-3 text-sm text-muted">For now, the priority is keeping routing, authentication state, and navigation stable before filling in each page's functionality.</p>
            </>
          )}
        </section>
      </div>
    </div>
  )
}
