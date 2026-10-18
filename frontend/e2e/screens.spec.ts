import { expect, test } from '@playwright/test'

/**
 * Browser end-to-end checks for the operating screens (the plan's screen order):
 * Proposals, Balance sheet, Compare, What-if, Evidence and decision, Tracker, Memo.
 * Needs the full stack behind the VITE dev server (or a preview): Postgres, Redis,
 * rules engine on :8000 and the gateway on :8080.
 *
 * Writes: the tracker test evaluates the default scenario (one `evaluations` row)
 * and generates one `pilot_plans` row. Locally, `./test.sh` cleanup (or `psql`)
 * clears these; in CI every job runs against a disposable service-container
 * database, so nothing leaks between runs.
 */

const screens: [string, RegExp][] = [
  ['/proposals', /pilot candidates/i],
  ['/balance-sheet', /burned/i],
  ['/compare', /what was spent/i],
  ['/what-if', /change one input/i],
  ['/evidence', /how strong the evidence/i],
  ['/tracker', /30-day plan/i],
  ['/memo', /one page for the decision record/i],
  ['/history', /what changed, and when/i],
]

for (const [route, heading] of screens) {
  test(`screen ${route} renders with its content`, async ({ page }) => {
    await page.goto(route)
    await expect(page.locator('h1')).toContainText(heading)
    await expect(page.locator('nav')).toContainText(/Decision workspace|Pilot tracker|Decision memo/i)
    // every rendered error notice means the stack is broken, not the check
    await expect(page.locator('.notice-bad')).toHaveCount(0)
  })
}

/**
 * The single-pilot tools run on the workspace's stored proposals. Each test stores its own proposal through the
 * API (no shipped sample exists) and finds it by its engine pilot id, P-<first 8 characters of the proposal id>.
 */
const created: string[] = []
test.afterEach(async ({ request }) => {
  // leave the database as the test found it
  while (created.length) await request.delete(`/api/v1/proposals/${created.pop()}`)
})

async function seedProposal(request: import('@playwright/test').APIRequestContext, label: string) {
  const name = `E2E ${label} ${Date.now()}`
  const res = await request.post('/api/v1/proposals', {
    data: { name, notes: 'Created by the browser test.', inputs: { setup_cost_inr: 200000, annual_benefit_inr: 300000, ai_running_cost_inr: 60000, benefit_type: 'cashable', readiness: 'medium', adoption_pct: 60 } },
  })
  expect(res.ok()).toBeTruthy()
  const p = (await res.json()) as { id: string }
  created.push(p.id)
  const pilot = `P-${p.id.slice(0, 8)}`
  return { name, pilot }
}

test('evidence screen shows the stored proposal with its stop rule, advice and checkpoint', async ({ page, request }) => {
  const { pilot } = await seedProposal(request, 'evidence')
  await page.goto('/evidence')
  const card = page.getByTestId(`pilot-${pilot}`)
  await expect(card).toContainText('2,40,000', { timeout: 15_000 })
  await expect(card).toContainText('Stop rule:')
  await expect(card).toContainText('Advice:')
  await expect(card).toContainText('Checkpoint:')
})

test('what-if: base vs changed table and the sweep ladder both answer', async ({ page, request }) => {
  await seedProposal(request, 'whatif')
  await page.goto('/what-if')
  await page.getByRole('button', { name: 'Run what-if' }).click()
  await expect(page.locator('caption')).toContainText('What-if base against changed', { timeout: 15_000 })
  await page.getByRole('button', { name: 'Run sweep' }).click()
  await expect(page.locator('text=/Direction:.*monotonic:/')).toBeVisible({ timeout: 15_000 })
})

test('tracker: plan, progress and reallocation recommendation for a stored proposal', async ({ page, request }) => {
  const { name, pilot } = await seedProposal(request, 'tracker')
  // an evaluation that includes the pilot makes the reallocation source exist
  const ev = await request.post('/api/v1/evaluate', {
    data: {
      budget: 600000,
      fixture: { records: { pilots: [{ id: pilot, task: name, setup_cost_inr: 200000, annual_running_cost_inr: 60000, annual_benefit_inr: 300000, benefit_type: 'cashable assumption', overlap_group: '' }] } },
      extension: { defaults: {}, pilots: [{ id: pilot, description: '', adoption_pct: 60, readiness: 'medium', observed_adoption_pct: null, observed_quality_pct: null }] },
    },
  })
  expect(ev.ok()).toBeTruthy()
  await page.goto('/tracker')
  await page.locator('#tp-pilot').selectOption(pilot)
  await page.getByRole('button', { name: 'Generate plan' }).click()
  await expect(page.getByTestId('plan-progress')).toContainText(`Tracker — ${pilot}`, { timeout: 15_000 })
  await expect(page.getByTestId('reallocation')).toHaveCount(0)
  await page.getByRole('button', { name: /Reallocation preview/ }).click()
  await expect(page.getByTestId('reallocation')).toContainText('Recommendation only', { timeout: 15_000 })
  await expect(page.getByTestId('reallocation')).toContainText('2,00,000')
})

test('memo renders the one-page markdown with the plan and audit sections', async ({ page, request }) => {
  const { pilot } = await seedProposal(request, 'memo')
  await page.goto('/memo')
  await page.locator('#memo-pilot').selectOption(pilot)
  await page.getByRole('button', { name: 'Generate', exact: true }).first().click()
  const memo = page.getByTestId('memo-text')
  await expect(memo).toContainText(`Decision memo — ${pilot}`, { timeout: 15_000 })
  await expect(memo).toContainText('## 30-day plan')
  await expect(memo).toContainText('## Audit trail')
})

test('add proposal: the import tab reads a CSV through the engine and stores the chosen rows', async ({ page, request }) => {
  const id = `IMP${Date.now()}`
  const csv = `id,task,setup_cost_inr,annual_benefit_inr,annual_running_cost_inr,benefit_type
${id},Import ok ${id},100000,150000,30000,cashable
${id}b,Import bad ${id},abc,1,,cashable
`
  await page.goto('/ai-proposals')
  await page.getByRole('button', { name: 'Import proposals' }).first().click()
  await page.locator('#pi-file').setInputFiles({ name: 'proposals.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  await expect(page.getByText('1 proposal found')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('summary', { hasText: /1 row the importer rejected/ })).toBeVisible()
  await page.getByRole('button', { name: /Store 1 proposal/ }).click()
  await expect(page.getByText('Stored (inputs v1)')).toBeVisible({ timeout: 15_000 })
  await page.getByRole('button', { name: 'Done' }).click()
  await expect(page.getByText(`Import ok ${id}`).first()).toBeVisible()
  const list = (await (await request.get('/api/v1/proposals')).json()) as { proposals: { id: string; name: string }[] }
  for (const p of list.proposals) if (p.name === `Import ok ${id}`) created.push(p.id)
})
