import type { UploadStatus } from '../../api/viewTypes'
import { Pill } from '../../components/fynd/Page'
import type { Tone } from '../../domain/fynd'

/** The upload stages of migration 00011: received → processing → parsed | failed → confirmed. */
export const UPLOAD_STATUS: Partial<Record<UploadStatus, { label: string; tone: Tone }>> = {
  pending_upload: { label: 'Awaiting upload', tone: 'off' },
  uploaded: { label: 'Uploaded', tone: 'info' },
  received: { label: 'Waiting', tone: 'off' },
  processing: { label: 'In progress', tone: 'info' },
  parsed: { label: 'Needs review', tone: 'warn' },
  failed: { label: 'Failed', tone: 'bad' },
  confirmed: { label: 'Confirmed', tone: 'ok' },
}

export function UploadStatusPill({ status }: { status: UploadStatus }) {
  const s = UPLOAD_STATUS[status] ?? { label: status, tone: 'off' as Tone }
  return <Pill tone={s.tone}>{s.label}</Pill>
}
