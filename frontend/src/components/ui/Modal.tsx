import { useEffect, useRef } from 'react'

interface Props {
  open: boolean
  onClose: () => void
  labelledBy: string
  children: React.ReactNode
}

/** Native <dialog>: focus is trapped while open, Escape closes it, and focus returns to the opener. */
export function Modal({ open, onClose, labelledBy, children }: Props) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) d.showModal()
    if (!open && d.open) d.close()
  }, [open])
  return (
    <dialog ref={ref} className="dialog" aria-labelledby={labelledBy} onClose={onClose} onCancel={(e) => { e.preventDefault(); onClose() }}>
      {open ? children : null}
    </dialog>
  )
}
