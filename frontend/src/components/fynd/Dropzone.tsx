import { useRef, useState } from 'react'
import { Icon } from '../ui/Icon'

export const fmtBytes = (n: number) => (n >= 1024 * 1024 ? `${(n / 1024 / 1024).toFixed(1)} MB` : n >= 1024 ? `${Math.round(n / 1024)} KB` : `${n} bytes`)

/** Drag-and-drop plus a real file picker. It only hands the file over; the caller validates and uploads. */
export function Dropzone({ accept, onFile, hint, disabled, id = 'fynd-file' }: { accept: string; onFile: (f: File) => void; hint: React.ReactNode; disabled?: boolean; id?: string }) {
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  return (
    <div
      className={`drop${over ? ' over' : ''}`}
      onDragOver={(e) => { e.preventDefault(); if (!disabled) setOver(true) }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => { e.preventDefault(); setOver(false); const f = e.dataTransfer.files?.[0]; if (f && !disabled) onFile(f) }}
    >
      <Icon name="upload" />
      <div className="stack" style={{ gap: 4 }}>
        <strong>Drag a document here</strong>
        <span className="caption">or</span>
      </div>
      <label htmlFor={id} className="btn btn-primary" aria-disabled={disabled} style={disabled ? { opacity: 0.5, pointerEvents: 'none' } : undefined}>Choose a file</label>
      <input ref={input} id={id} type="file" accept={accept} className="sr" disabled={disabled}
        onChange={(e) => { const f = e.target.files?.[0]; if (f) onFile(f); if (input.current) input.current.value = '' }} />
      <span className="hint" style={{ maxWidth: '60ch' }}>{hint}</span>
    </div>
  )
}
