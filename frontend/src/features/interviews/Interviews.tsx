import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { Interview } from '../../api/types'
import { parseAmount } from '../../domain/draft'
import { formatDate } from '../../domain/format'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { TextArea, TextField } from '../../components/ui/Fields'

type Errs = Partial<Record<'role' | 'task' | 'minutes' | 'date' | 'result' | 'quote', string>>
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

export function Interviews() {
  const [list, setList] = useState<Interview[] | null>(null)
  const [loadError, setLoadError] = useState<unknown>(null)
  const [role, setRole] = useState('')
  const [task, setTask] = useState('')
  const [minutes, setMinutes] = useState('')
  const [result, setResult] = useState('')
  const [quote, setQuote] = useState('')
  const [date, setDate] = useState('')
  const [errs, setErrs] = useState<Errs>({})
  const [busy, setBusy] = useState(false)
  const [fail, setFail] = useState<unknown>(null)
  const [ok, setOk] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try { setList(await api.listInterviews()); setLoadError(null) } catch (e) { setLoadError(e) }
  }, [])
  useEffect(() => { void refresh() }, [refresh])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setOk(null); setFail(null)
    const x: Errs = {}
    const r = role.trim(), t = task.trim()
    if (r === '' || r.length > 100) x.role = 'Enter a role of up to 100 characters. Use a role, not a name.'
    if (t === '' || t.length > 200) x.task = 'Describe the task in up to 200 characters.'
    let m: number | null = null
    if (minutes.trim() !== '') { m = parseAmount(minutes); if (!Number.isFinite(m) || m < 0 || m > 10000) { x.minutes = 'Minutes must be between 0 and 10,000.'; m = null } }
    if (date.trim() !== '' && !DATE_RE.test(date.trim())) x.date = 'Use the format YYYY-MM-DD.'
    setErrs(x)
    if (Object.keys(x).length) return
    setBusy(true)
    try {
      const saved = (await api.addInterview({
        interviewee_role: r, task: t, minutes_per_task: m, useful_result: result.trim() || null, quote: quote.trim() || null, interviewed_on: date.trim() || null,
      })).data
      setOk(`Recorded interview ${saved.id}. The service confirmed it.`)
      setRole(''); setTask(''); setMinutes(''); setResult(''); setQuote(''); setDate('')
      await refresh()
    } catch (err) {
      if (err instanceof ApiError && err.field) {
        const map: Record<string, keyof Errs> = { interviewee_role: 'role', task: 'task', minutes_per_task: 'minutes', interviewed_on: 'date' }
        if (map[err.field]) setErrs({ [map[err.field]]: err.message })
      }
      setFail(err)
    } finally { setBusy(false) }
  }

  return (
    <div className="stack" style={{ gap: 36 }}>
      <form className="panel" onSubmit={submit} noValidate>
        <h2 className="h3">Record an interview</h2>
        <div className="notice notice-info" role="note"><div>Enter the interviewee’s <strong>role</strong>, for example “Accounts payable lead”. Never enter a person’s name. Avoid names inside the quote as well.</div></div>
        <div className="form-grid">
          <TextField id="iv-role" label="Interviewee role" required value={role} onChange={(v) => { setRole(v); setErrs((e) => ({ ...e, role: undefined })) }} error={errs.role} maxLength={120} />
          <TextField id="iv-date" label="Interview date" value={date} onChange={(v) => { setDate(v); setErrs((e) => ({ ...e, date: undefined })) }} error={errs.date} placeholder="YYYY-MM-DD" />
          <TextField id="iv-task" label="Task discussed" required value={task} onChange={(v) => { setTask(v); setErrs((e) => ({ ...e, task: undefined })) }} error={errs.task} maxLength={220} />
          <TextField id="iv-min" label="Time the task takes" unit="min" inputMode="decimal" value={minutes} onChange={(v) => { setMinutes(v); setErrs((e) => ({ ...e, minutes: undefined })) }} error={errs.minutes} hint="Minutes per task. Leave blank if not stated; blank is not zero." />
        </div>
        <TextArea id="iv-result" label="What a useful result looks like" value={result} onChange={setResult} />
        <TextArea id="iv-quote" label="A short quote" value={quote} onChange={setQuote} />
        <div aria-live="polite" className="stack" style={{ gap: 8 }}>
          {fail != null && <ErrorNotice title="Not recorded." error={fail} />}
          {ok && <div className="notice notice-ok"><div>{ok}</div></div>}
        </div>
        <div><button className="btn btn-primary" type="submit" disabled={busy}>{busy ? 'Recording…' : 'Record interview'}</button></div>
      </form>

      <div className="stack">
        <h2 className="h3">Recorded interviews</h2>
        {loadError != null && <ErrorNotice title="Interviews could not be loaded." error={loadError} onRetry={() => void refresh()} />}
        {loadError == null && list === null && <Skeleton rows={3} height={48} />}
        {loadError == null && list?.length === 0 && <Empty title="No interviews recorded">Interviews you record appear here.</Empty>}
        {list && list.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Recorded interviews</caption>
            <thead><tr><th scope="col">Date</th><th className="sticky" scope="col">Role</th><th scope="col">Task</th><th className="num" scope="col">Minutes</th><th scope="col">Useful result</th><th scope="col">Quote</th></tr></thead>
            <tbody>{list.map((i) => (
              <tr key={i.id}>
                <td>{i.interviewed_on ? formatDate(i.interviewed_on) : <span className="muted">Not provided</span>}</td>
                <td className="sticky strong">{i.interviewee_role}</td>
                <td className="wrap-cell">{i.task}</td>
                <td className="num">{i.minutes_per_task === null ? <span className="muted">Not provided</span> : i.minutes_per_task}</td>
                <td className="wrap-cell">{i.useful_result ?? <span className="muted">Not provided</span>}</td>
                <td className="wrap-cell">{i.quote ?? <span className="muted">Not provided</span>}</td>
              </tr>))}</tbody>
          </table></div>
        )}
      </div>
    </div>
  )
}
