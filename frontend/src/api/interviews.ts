import type { Interview, InterviewIn } from './types'
import { V1, json } from './http'

export const addInterview = (b: InterviewIn) => json<Interview>('POST', `${V1}/interviews`, b)
export const listInterviews = async () => (await json<{ interviews: Interview[] | null }>('GET', `${V1}/interviews`)).data.interviews ?? []
