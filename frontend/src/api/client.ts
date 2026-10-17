/**
 * The only place the frontend talks to the network. Handlers are split by resource
 * (core, workspace, observations, compare, portfolios, history, lifecycle, proposals, interviews);
 * this barrel keeps the flat `api` shape every screen already uses. Every route here
 * exists in backend/internal/httpapi/router.go.
 */
import * as core from './core'
import * as workspace from './workspace'
import * as observations from './observations'
import * as compare from './compare'
import * as portfolios from './portfolios'
import * as history from './history'
import * as lifecycle from './lifecycle'
import * as proposals from './proposals'
import * as interviews from './interviews'

export * from './http' // ApiError, describeError, parseErrorEnvelope, saveDownload, Download

export const api = {
  ...core,
  ...workspace,
  ...observations,
  ...compare,
  ...portfolios,
  ...history,
  ...lifecycle,
  ...proposals,
  ...interviews,
}
