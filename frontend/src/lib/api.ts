import axios from 'axios'
import { computed, ref } from 'vue'
import { currentUser, safeReturnTo } from './auth'
import type { Tournament } from './types'

export const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
  headers: { 'X-TAS-CSRF': '1' },
})
export const publicApi = axios.create({ baseURL: '/api/public' })
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      currentUser.value = null
      const returnTo = safeReturnTo(window.location.hash.slice(1))
      if (!window.location.hash.startsWith('#/login'))
        window.location.hash = `/login?returnTo=${encodeURIComponent(returnTo)}`
    }
    return Promise.reject(error)
  },
)
export function errorMessage(error: unknown): string {
  if (axios.isAxiosError(error))
    return String(
      error.response?.data?.error || error.message || 'The request could not be completed.',
    )
  return error instanceof Error ? error.message : 'The request could not be completed.'
}
export function hasRole(...roles: string[]): boolean {
  return (currentUser.value?.roles ?? []).some((role) => roles.includes(role.toLowerCase()))
}
export const canAdmin = computed(() => hasRole('admin'))
export const canEdit = computed(() => hasRole('admin', 'editor'))
export const canReferee = computed(() => hasRole('admin', 'editor', 'referee'))
export const tournaments = ref<Tournament[]>([])
export const activeTournamentId = ref<number | null>(
  Number(localStorage.getItem('activeTournament')) || null,
)
export const activeTournament = computed(() =>
  tournaments.value.find((t) => t.id === activeTournamentId.value),
)
export function selectTournament(id: number) {
  activeTournamentId.value = id
  localStorage.setItem('activeTournament', String(id))
}
export async function loadTournaments() {
  tournaments.value = (await api.get<Tournament[]>('/tournaments')).data
  if (!tournaments.value.length) {
    activeTournamentId.value = null
    localStorage.removeItem('activeTournament')
  }
  if (!tournaments.value.some((t) => t.id === activeTournamentId.value) && tournaments.value[0])
    selectTournament(tournaments.value[0].id)
}
export function formatDate(
  value: string | null | undefined,
  timezone?: string,
  options: Intl.DateTimeFormatOptions = {},
) {
  if (!value) return 'Not scheduled'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    timeZone: timezone,
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    ...options,
  }).format(date)
}
export function localDateTime(value: string) {
  const d = new Date(value)
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}
