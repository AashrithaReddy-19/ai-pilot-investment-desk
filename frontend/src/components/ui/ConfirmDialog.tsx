import { Modal } from './Modal'

interface Props {
  open: boolean
  title: string
  body: React.ReactNode
  confirmLabel: string
  busy?: boolean
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({ open, title, body, confirmLabel, busy, danger, onConfirm, onCancel }: Props) {
  return (
    <Modal open={open} onClose={onCancel} labelledBy="confirm-title">
      <div className="dlg">
        <div className="dlg-head"><h2 className="h2" id="confirm-title">{title}</h2></div>
        <div className="dlg-body"><div className="prose">{body}</div></div>
        <div className="dlg-foot">
          <span className="grow" />
          <button className="btn" onClick={onCancel} disabled={busy} autoFocus>Cancel</button>
          <button className={`btn ${danger ? 'btn-danger' : 'btn-primary'}`} onClick={onConfirm} disabled={busy}>{busy ? 'Working…' : confirmLabel}</button>
        </div>
      </div>
    </Modal>
  )
}
