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

test('public check 1 holds in a browser: AI1 net 1,20,000/yr on the evidence screen', async ({ page }) => {
  await page.goto('/evidence')
  const pilot = page.getByTestId('pilot-AI1')
  await expect(pilot).toContainText('1,20,000', { timeout: 15_000 })
  await expect(pilot).toContainText('Stop rule:')
  await expect(pilot).toContainText('Advice:')
  await expect(pilot).toContainText('Checkpoint:')
})

test('what-if: base vs changed table and the sweep ladder both answer', async ({ page }) => {
  await page.goto('/what-if')
  await page.getByRole('button', { name: 'Run what-if' }).click()
  await expect(page.locator('caption')).toContainText('What-if base against changed', { timeout: 15_000 })
  await page.getByRole('button', { name: 'Run sweep' }).click()
  await expect(page.locator('text=/Direction:.*monotonic:/')).toBeVisible({ timeout: 15_000 })
})

test('tracker: plan, progress and reallocation recommendation for AI1', async ({ page, request }) => {
  // an evaluation makes the default reallocation source exist
  const ev = await request.post('/api/v1/evaluate', { data: { budget: 600000 } })
  expect(ev.ok()).toBeTruthy()
  await page.goto('/tracker')
  await page.getByRole('button', { name: 'Generate plan' }).click()
  await expect(page.getByTestId('plan-progress')).toContainText('Tracker — AI1', { timeout: 15_000 })
  await expect(page.getByTestId('reallocation')).toHaveCount(0)
  await page.getByRole('button', { name: /Reallocation preview/ }).click()
  await expect(page.getByTestId('reallocation')).toContainText('Recommendation only', { timeout: 15_000 })
  await expect(page.getByTestId('reallocation')).toContainText('2,00,000')
})

test('memo renders the one-page markdown with provenance tags and the plan section', async ({ page }) => {
  await page.goto('/memo')
  await page.getByRole('button', { name: 'Generate', exact: true }).first().click()
  const memo = page.getByTestId('memo-text')
  await expect(memo).toContainText('Decision memo — AI1', { timeout: 15_000 })
  await expect(memo).toContainText('synthetic')
  await expect(memo).toContainText('## 30-day plan')
  await expect(memo).toContainText('## Audit trail')
})
