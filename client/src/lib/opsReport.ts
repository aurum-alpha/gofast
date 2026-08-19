/** Ops report schedule + archive API helpers. */

export type OpsReportSchedule = {
  enabled: boolean
  timezone: string
  send_at: string
  last_success_at?: string
  last_success_local?: string
  last_error?: string
  last_error_at?: string
  next_at?: string
  refresh_tallies?: Record<string, { successes: number; failures: number }>
}

export type OpsReportArchiveMeta = {
  id: string
  kind: string
  generated_at: string
  subject: string
  filename: string
}

async function readError(res: Response): Promise<string> {
  const text = await res.text()
  return text || `${res.status} ${res.statusText}`
}

export async function fetchOpsSchedule(): Promise<OpsReportSchedule> {
  const res = await fetch('/api/ops-report/schedule')
  if (!res.ok) throw new Error(await readError(res))
  return (await res.json()) as OpsReportSchedule
}

export async function fetchOpsArchives(): Promise<OpsReportArchiveMeta[]> {
  const res = await fetch('/api/ops-report/archives')
  if (!res.ok) throw new Error(await readError(res))
  const body = (await res.json()) as { archives?: OpsReportArchiveMeta[] }
  return body.archives ?? []
}

export async function testOpsSMTP(): Promise<void> {
  const res = await fetch('/api/ops-report/test-smtp', { method: 'POST' })
  if (!res.ok) throw new Error(await readError(res))
}

export async function sendOpsPreview(): Promise<OpsReportArchiveMeta> {
  const res = await fetch('/api/ops-report/send-preview', { method: 'POST' })
  if (!res.ok) throw new Error(await readError(res))
  return (await res.json()) as OpsReportArchiveMeta
}

export async function resendOpsArchive(id: string): Promise<void> {
  const res = await fetch(`/api/ops-report/archives/${encodeURIComponent(id)}/resend`, {
    method: 'POST',
  })
  if (!res.ok) throw new Error(await readError(res))
}
