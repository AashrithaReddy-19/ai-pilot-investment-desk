interface Base {
  id: string
  label: string
  required?: boolean
  hint?: string
  error?: string
}

function Label({ id, label, required }: Pick<Base, 'id' | 'label' | 'required'>) {
  return (
    <label className="label" htmlFor={id}>
      {label}
      <span className={`req${required ? ' req-yes' : ''}`}>{required ? 'required' : 'optional'}</span>
    </label>
  )
}

function Below({ id, hint, error }: Pick<Base, 'id' | 'hint' | 'error'>) {
  if (error) return <span id={`${id}-msg`} className="field-error" role="alert">{error}</span>
  return hint ? <span id={`${id}-msg`} className="hint">{hint}</span> : null
}

const describedBy = (b: Base) => (b.error || b.hint ? `${b.id}-msg` : undefined)

interface TextProps extends Base {
  value: string
  onChange: (v: string) => void
  unit?: string
  inputMode?: 'numeric' | 'decimal' | 'text'
  placeholder?: string
  autoFocus?: boolean
  maxLength?: number
  type?: string
}

export function TextField(p: TextProps) {
  const input = (
    <input
      id={p.id} className="input" value={p.value} onChange={(e) => p.onChange(e.target.value)} inputMode={p.inputMode}
      placeholder={p.placeholder} autoFocus={p.autoFocus} maxLength={p.maxLength} type={p.type ?? 'text'} autoComplete="off"
      aria-invalid={p.error ? true : undefined} aria-required={p.required || undefined} aria-describedby={describedBy(p)}
    />
  )
  return (
    <div className="field">
      <Label {...p} />
      {p.unit ? <div className="input-affix">{input}<span className="unit" aria-hidden>{p.unit}</span></div> : input}
      <Below {...p} />
    </div>
  )
}

interface AreaProps extends Base {
  value: string
  onChange: (v: string) => void
  rows?: number
  maxLength?: number
}

export function TextArea(p: AreaProps) {
  return (
    <div className="field">
      <Label {...p} />
      <textarea
        id={p.id} className="textarea" rows={p.rows ?? 3} value={p.value} maxLength={p.maxLength} onChange={(e) => p.onChange(e.target.value)}
        aria-invalid={p.error ? true : undefined} aria-describedby={describedBy(p)}
      />
      <Below {...p} />
    </div>
  )
}

interface SelectProps extends Base {
  value: string
  onChange: (v: string) => void
  options: { value: string; label: string }[]
}

export function SelectField(p: SelectProps) {
  return (
    <div className="field">
      <Label {...p} />
      <select id={p.id} className="select" value={p.value} onChange={(e) => p.onChange(e.target.value)} aria-invalid={p.error ? true : undefined} aria-describedby={describedBy(p)}>
        {p.options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
      <Below {...p} />
    </div>
  )
}
