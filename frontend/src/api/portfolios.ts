import type { ScenarioBody, PortfolioSummary, SavedPortfolio } from './types'
import { V1, enc, json, file } from './http'
import type { Download } from './http'

export const savePortfolio = (b: ScenarioBody & { name: string }) => json<SavedPortfolio>('POST', `${V1}/portfolios`, b)
export const listPortfolios = async () => (await json<{ portfolios: PortfolioSummary[] | null }>('GET', `${V1}/portfolios`)).data.portfolios ?? []
export const getPortfolio = (name: string) => json<SavedPortfolio>('GET', `${V1}/portfolios/${enc(name)}`)
export const deletePortfolio = (name: string) => json<void>('DELETE', `${V1}/portfolios/${enc(name)}`)
export const exportCsv = (b: ScenarioBody): Promise<Download> => file(`${V1}/export/csv`, b, 'pilots.csv')
export const exportEvidence = (b: ScenarioBody): Promise<Download> => file(`${V1}/export/evidence`, b, 'evidence.md')
