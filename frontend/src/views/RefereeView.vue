<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { activeTournamentId, api, canEdit, selectTournament } from '@/lib/api'
import { currentUser } from '@/lib/auth'
import type { Tournament, Workspace } from '@/lib/types'

const route = useRoute()
const tournaments = ref<Tournament[]>([])
const selected = ref(Number(route.query.tournament) || activeTournamentId.value || 0)
const field = ref<number | ''>('')
const workspace = ref<Workspace | null>(null)
const loading = ref(true)
const error = ref('')
let poll: ReturnType<typeof setInterval> | undefined
let loadSequence = 0
const eligibleLeagues = computed(
  () =>
    new Set(
      workspace.value?.leagues.filter((l) => l.referee_mode && l.mode === 'full').map((l) => l.id),
    ),
)
const refereeIds = computed(
  () =>
    new Set(
      workspace.value?.referees.filter((r) => r.user_id === currentUser.value?.id).map((r) => r.id),
    ),
)
const fields = computed(
  () =>
    workspace.value?.fields.filter(
      (f) =>
        eligibleLeagues.value.has(f.league_id) ||
        workspace.value?.leagues.some((l) => l.parent_id === f.league_id && l.referee_mode),
    ) ?? [],
)
const matches = computed(() =>
  (workspace.value?.matches ?? [])
    .filter(
      (m) =>
        !m.is_placeholder &&
        eligibleLeagues.value.has(m.league_id) &&
        (field.value === '' || m.field_id === field.value) &&
        !['approved'].includes(m.status) &&
        m.team2_id &&
        (canEdit.value || m.referee_ids.some((id) => refereeIds.value.has(id))),
    )
    .sort(
      (a, b) =>
        (a.scheduled_at || '9999').localeCompare(b.scheduled_at || '9999') || a.number - b.number,
    ),
)
const teamName = (id: number | null) =>
  workspace.value?.teams.find((t) => t.id === id)?.name || 'Bye'
const fieldName = (id: number | null) =>
  workspace.value?.fields.find((f) => f.id === id)?.name || 'Unassigned field'
const leagueName = (id: number) => workspace.value?.leagues.find((l) => l.id === id)?.name || ''
function time(value: string | null) {
  if (!value) return 'Unscheduled'
  return new Date(value).toLocaleString([], {
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    timeZone: workspace.value?.tournament.timezone || undefined,
  })
}
function message(err: unknown) {
  return (
    (err as { response?: { data?: { error?: string } } }).response?.data?.error ||
    (err as Error).message ||
    'Could not load referee assignments.'
  )
}
async function load() {
  const id = selected.value
  const sequence = ++loadSequence
  if (!selected.value) {
    workspace.value = null
    loading.value = false
    return
  }
  try {
    const response = await api.get<Workspace>(`/tournaments/${id}/workspace`)
    if (sequence !== loadSequence || id !== selected.value) return
    workspace.value = response.data
    selectTournament(selected.value)
    error.value = ''
  } catch (err) {
    if (sequence === loadSequence && id === selected.value) error.value = message(err)
  } finally {
    if (sequence === loadSequence && id === selected.value) loading.value = false
  }
}
async function changeTournament() {
  field.value = ''
  loading.value = true
  await load()
}
watch(activeTournamentId, async (id) => {
  if (id && id !== selected.value) {
    selected.value = id
    await changeTournament()
  }
})
onMounted(async () => {
  try {
    tournaments.value = (await api.get<Tournament[]>('/tournaments')).data
    if (!tournaments.value.some((t) => t.id === selected.value))
      selected.value = tournaments.value[0]?.id || 0
    await load()
    poll = setInterval(() => {
      if (!document.hidden) void load()
    }, 15_000)
  } catch (err) {
    error.value = message(err)
    loading.value = false
  }
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="referee-page">
    <header class="referee-header">
      <div>
        <p class="eyebrow">MATCH OFFICIALS</p>
        <h1>Referee desk</h1>
        <p>Select a field, check the robots, and take the next match.</p>
      </div>
      <span class="live-label"><i /> Updates every 15 seconds</span>
    </header>
    <div class="referee-filters">
      <label
        >Tournament<select v-model="selected" @change="changeTournament">
          <option v-if="!tournaments.length" :value="0">No tournaments yet</option>
          <option v-for="t in tournaments" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select></label
      >
      <label
        >Field<select v-model="field">
          <option value="">All fields</option>
          <option v-for="f in fields" :key="f.id" :value="f.id">{{ f.name }}</option>
        </select></label
      >
      <button type="button" class="refresh" @click="load">Refresh</button>
    </div>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <p v-if="workspace && !canEdit && !refereeIds.size" class="error-message" role="status">
      Your account is not linked to a referee in this tournament. Ask an administrator to add your
      user ID <strong>{{ currentUser?.id }}</strong> to your referee record and assign you to a
      match.
    </p>
    <p v-if="loading" class="referee-empty">Loading assignments…</p>
    <div v-else-if="!matches.length" class="referee-empty">
      <span class="empty-symbol">✓</span>
      <h2>No upcoming matches</h2>
      <p>
        Matches appear here when referee mode is enabled for their league. Try another field or
        tournament.
      </p>
    </div>
    <div v-else class="match-list">
      <RouterLink v-for="m in matches" :key="m.id" :to="`/referee/${m.id}`" class="assignment">
        <div class="assignment-time">
          <strong>{{ time(m.scheduled_at) }}</strong
          ><span>{{ fieldName(m.field_id) }}</span>
        </div>
        <div class="assignment-teams">
          <small>{{ leagueName(m.league_id) }} · Match {{ m.number }}</small
          ><strong>{{ teamName(m.team1_id) }} <span>vs</span> {{ teamName(m.team2_id) }}</strong>
        </div>
        <span class="assignment-status" :class="m.status">{{ m.status.replaceAll('_', ' ') }}</span
        ><span class="assignment-arrow">↗</span>
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.referee-page {
  max-width: 1180px;
  margin: 0 auto;
}
.referee-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  margin-bottom: 28px;
}
.eyebrow {
  font-size: 11px;
  letter-spacing: 0.16em;
  font-weight: 800;
  color: #098879;
  margin: 0 0 8px;
}
h1 {
  font-size: 32px;
  letter-spacing: -1px;
  margin: 0 0 8px;
  color: #193349;
}
.referee-header p:last-child {
  color: #6c7f90;
  margin: 0;
}
.live-label {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: #6c7f90;
}
.live-label i {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #11aa8b;
}
.referee-filters {
  display: flex;
  gap: 16px;
  align-items: flex-end;
  background: white;
  border: 1px solid #dde6eb;
  border-radius: 12px;
  padding: 20px;
  margin-bottom: 20px;
}
.referee-filters label {
  display: grid;
  gap: 7px;
  font-size: 12px;
  color: #547084;
  font-weight: 700;
  min-width: 200px;
}
.referee-filters select {
  border: 1px solid #cad7e0;
  background: #fff;
  color: #193349;
  border-radius: 7px;
  padding: 10px;
  max-width: 100%;
}
.refresh {
  margin-left: auto;
  padding: 10px 18px;
  border: 1px solid #cad7e0;
  border-radius: 7px;
  background: white;
  color: #274b60;
  cursor: pointer;
}
.match-list {
  display: grid;
  gap: 12px;
}
.assignment {
  display: flex;
  align-items: center;
  gap: 24px;
  background: white;
  padding: 22px;
  border: 1px solid #dde6eb;
  border-radius: 12px;
  text-decoration: none;
  color: #193349;
  transition:
    border-color 0.2s,
    transform 0.2s;
}
.assignment:hover {
  border-color: #1ca28f;
  transform: translateY(-1px);
}
.assignment-time {
  min-width: 150px;
  display: grid;
  gap: 7px;
  font-size: 14px;
}
.assignment-time span {
  color: #6b8294;
  font-size: 12px;
}
.assignment-teams {
  display: grid;
  gap: 9px;
  flex: 1;
}
.assignment-teams small {
  color: #6b8294;
}
.assignment-teams strong {
  font-size: 18px;
}
.assignment-teams strong span {
  color: #9cadb8;
  padding: 0 7px;
  font-size: 12px;
  font-weight: 500;
}
.assignment-status {
  background: #edf3f6;
  border-radius: 20px;
  padding: 6px 12px;
  font-size: 11px;
  text-transform: capitalize;
  white-space: nowrap;
}
.assignment-status.in_progress {
  background: #e3f7f1;
  color: #00755e;
}
.assignment-status.submitted {
  background: #fff2d6;
  color: #946509;
}
.assignment-arrow {
  color: #089680;
  font-size: 24px;
}
.referee-empty {
  text-align: center;
  padding: 70px 24px;
  background: white;
  border: 1px dashed #c8d8e0;
  border-radius: 12px;
  color: #6b8294;
}
.referee-empty h2 {
  color: #24465c;
  font-size: 20px;
}
.empty-symbol {
  color: #13a18a;
  font-size: 40px;
}
.error-message {
  background: #fff0ed;
  border: 1px solid #f4c5bb;
  border-radius: 8px;
  padding: 14px;
  color: #ad3f27;
}
@media (max-width: 700px) {
  .referee-header {
    align-items: flex-start;
    flex-direction: column;
  }
  .referee-filters {
    flex-wrap: wrap;
  }
  .referee-filters label {
    flex: 1;
    min-width: 140px;
  }
  .assignment {
    gap: 14px;
    flex-wrap: wrap;
  }
  .assignment-time {
    min-width: 100%;
    display: flex;
    justify-content: space-between;
  }
  .assignment-teams strong {
    font-size: 16px;
  }
  .assignment-arrow {
    display: none;
  }
  .live-label {
    display: none;
  }
}
</style>
