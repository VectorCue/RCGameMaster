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
  let loadSequence = 0
  async function reload() {
    const id = tournamentId.value
    const sequence = ++loadSequence
    loading.value = true
    error.value = ''
    try {
      const response = await api.get<Workspace>(`/tournaments/${id}/workspace`)
      if (sequence !== loadSequence || id !== tournamentId.value) return
      workspace.value = response.data
      selectTournament(id)
    } catch (e) {
      if (sequence === loadSequence && id === tournamentId.value) error.value = errorMessage(e)
    } finally {
      if (sequence === loadSequence && id === tournamentId.value) loading.value = false
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
  watch(
    tournamentId,
    () => {
      workspace.value = null
      void reload()
    },
    { immediate: true },
  )
  const teamName = (id: number | null) =>
    workspace.value?.teams.find((t) => t.id === id)?.name ??
    (id === 0 ? 'Awaiting teams' : id ? `Team ${id}` : 'Bye')
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
