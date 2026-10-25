import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, errorMessage, selectTournament } from '@/lib/api'
import type { Workspace } from '@/lib/types'
export function useWorkspace() {
  const route = useRoute()
  const workspace = ref<Workspace | null>(null)
  const loading = ref(true)
  const error = ref('')
  const busy = ref(false)
  const tournamentId = computed(() => Number(route.params.id))
  async function reload() {
    loading.value = true
    error.value = ''
    try {
      workspace.value = (
        await api.get<Workspace>(`/tournaments/${tournamentId.value}/workspace`)
      ).data
      selectTournament(tournamentId.value)
    } catch (e) {
      error.value = errorMessage(e)
    } finally {
      loading.value = false
    }
  }
  async function perform(action: () => Promise<unknown>) {
    busy.value = true
    error.value = ''
    try {
      await action()
      await reload()
      return true
    } catch (e) {
      error.value = errorMessage(e)
      return false
    } finally {
      busy.value = false
    }
  }
  watch(tournamentId, reload, { immediate: true })
  const teamName = (id: number | null) =>
    workspace.value?.teams.find((t) => t.id === id)?.name ?? (id ? `Team ${id}` : 'Bye')
  const leagueName = (id: number) =>
    workspace.value?.leagues.find((l) => l.id === id)?.name ?? 'League'
  const stageName = (id: number) =>
    workspace.value?.stages.find((s) => s.id === id)?.name ?? 'Stage'
  return {
    workspace,
    loading,
    error,
    busy,
    tournamentId,
    reload,
    perform,
    teamName,
    leagueName,
    stageName,
  }
}
