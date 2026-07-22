export type SQLEditorWorkspaceSnapshot<TTab> = {
  ownerKey: string
  tabs: TTab[]
  activeTabId: string
  editorHeights: Record<string, string>
}

let snapshot: SQLEditorWorkspaceSnapshot<unknown> | null = null

export function getSQLEditorWorkspaceSnapshot<TTab>(ownerKey: string) {
  if (!snapshot || snapshot.ownerKey !== ownerKey) {
    return null
  }
  return snapshot as SQLEditorWorkspaceSnapshot<TTab>
}

export function saveSQLEditorWorkspaceSnapshot<TTab>(nextSnapshot: SQLEditorWorkspaceSnapshot<TTab>) {
  snapshot = nextSnapshot as SQLEditorWorkspaceSnapshot<unknown>
}

export function clearSQLEditorWorkspaceSnapshot() {
  snapshot = null
}
