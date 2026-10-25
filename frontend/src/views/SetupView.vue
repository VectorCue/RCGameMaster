<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useWorkspace } from '@/composables/workspace'
import { api, canAdmin, formatDate, loadTournaments, errorMessage } from '@/lib/api'
import type { League, Stage, Field, Team, Referee, Tournament } from '@/lib/types'
const { workspace: w, loading, error, busy, tournamentId, perform, leagueName } = useWorkspace()
const router = useRouter()
const tab = ref('main-leagues')
const notice = ref('')
const tabs = [
  { id: 'main-leagues', name: 'Main leagues' },
  { id: 'sub-leagues', name: 'Sub-leagues' },
  { id: 'stages', name: 'Stages' },
  { id: 'teams', name: 'Teams' },
  { id: 'fields', name: 'Fields & availability' },
  { id: 'referees', name: 'Referees' },
  { id: 'event', name: 'Event settings' },
]
const blankLeague = () => ({
  id: 0,
  name: '',
  abbrev: '',
  parent_id: null as number | null,
  mode: 'full' as 'full' | 'scheduling',
  referee_mode: true,
  inspections_enabled: true,
  robot_count: 2,
  max_weight_g: 1500,
  ball_zone_cm: 1.5,
  voltage: 48,
  size_cm: 22,
  motor_limit: 4,
  half_seconds: 600,
  halftime_seconds: 300,
})
const mainLeagues = computed(() => w.value?.leagues.filter((item) => !item.parent_id) ?? [])
const subLeagues = computed(() => w.value?.leagues.filter((item) => item.parent_id) ?? [])
const mainLeague = ref(blankLeague())
const subLeague = ref(blankLeague())
const returnToSubLeague = ref(false)
const parentLeague = computed(() =>
  mainLeagues.value.find((item) => item.id === subLeague.value.parent_id),
)
watch(parentLeague, (parent) => {
  if (!parent) return
  subLeague.value.mode = parent.mode
  subLeague.value.referee_mode = parent.referee_mode
})
watch(
  () => mainLeague.value.mode,
  (mode) => {
    if (mode === 'scheduling') mainLeague.value.referee_mode = false
  },
)
function chooseTab(id: string) {
  tab.value = id
  notice.value = ''
  error.value = ''
}
function addMainLeague() {
  mainLeague.value = blankLeague()
  returnToSubLeague.value = false
  chooseTab('main-leagues')
}
function addSubLeague(parentId = mainLeagues.value[0]?.id ?? null) {
  subLeague.value = { ...blankLeague(), parent_id: parentId }
  chooseTab('sub-leagues')
}
function editMainLeague(item: League, returnToSub = false) {
  mainLeague.value = { ...item, inspections_enabled: item.inspections_enabled !== false }
  returnToSubLeague.value = returnToSub
  chooseTab('main-leagues')
}
function editSubLeague(item: League) {
  subLeague.value = { ...item, inspections_enabled: item.inspections_enabled !== false }
  chooseTab('sub-leagues')
}
function cancelMainEdit() {
  mainLeague.value = blankLeague()
  tab.value = returnToSubLeague.value ? 'sub-leagues' : 'main-leagues'
  returnToSubLeague.value = false
  error.value = ''
  notice.value = 'Main league editing cancelled.'
}
function cancelSubEdit() {
  const parentId = subLeague.value.parent_id
  subLeague.value = { ...blankLeague(), parent_id: parentId }
  error.value = ''
  notice.value = 'Sub-league editing cancelled.'
}
async function saveMainLeague() {
  notice.value = ''
  const editing = Boolean(mainLeague.value.id)
  const data = {
    ...mainLeague.value,
    parent_id: null,
    referee_mode: mainLeague.value.mode === 'full' && mainLeague.value.referee_mode,
  }
  let saved: League | undefined
  if (
    !(await perform(async () => {
      saved = (
        editing
          ? await api.put<League>(`/leagues/${data.id}`, data)
          : await api.post<League>(`/tournaments/${tournamentId.value}/leagues`, data)
      ).data
    })) ||
    !saved
  )
    return
  mainLeague.value = blankLeague()
  if (!editing) {
    addSubLeague(saved.id)
    notice.value = `Main league “${saved.name}” created. Add its first sub-league below.`
  } else {
    if (returnToSubLeague.value) tab.value = 'sub-leagues'
    notice.value = `Main league “${saved.name}” updated. Its sub-leagues inherit the management settings.`
  }
  returnToSubLeague.value = false
}
async function saveSubLeague() {
  notice.value = ''
  if (!parentLeague.value) {
    error.value = 'Create and select a main league before saving a sub-league.'
    return
  }
  const editing = Boolean(subLeague.value.id)
  const data: Partial<ReturnType<typeof blankLeague>> = {
    ...subLeague.value,
    parent_id: parentLeague.value.id,
    mode: parentLeague.value.mode,
    referee_mode: parentLeague.value.referee_mode,
  }
  if (!subLeague.value.inspections_enabled) {
    for (const key of [
      'max_weight_g',
      'ball_zone_cm',
      'voltage',
      'size_cm',
      'motor_limit',
    ] as const) {
      const value = data[key]
      if (
        typeof value !== 'number' ||
        !Number.isFinite(value) ||
        value < 0 ||
        (key === 'motor_limit' && !Number.isSafeInteger(value))
      )
        delete data[key]
    }
  }
  if (!parentLeague.value.referee_mode) {
    for (const key of ['half_seconds', 'halftime_seconds'] as const) {
      const value = data[key]
      if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 1 || value > 86400)
        delete data[key]
    }
  }
  let saved: League | undefined
  if (
    !(await perform(async () => {
      saved = (
        editing
          ? await api.put<League>(`/leagues/${data.id}`, data)
          : await api.post<League>(`/tournaments/${tournamentId.value}/leagues`, data)
      ).data
    })) ||
    !saved
  )
    return
  if (!team.value.id) team.value.league_id = saved.id
  if (!stage.value.id) stage.value.league_id = saved.id
  subLeague.value = { ...blankLeague(), parent_id: data.parent_id ?? null }
  notice.value = `Sub-league “${data.name}” ${editing ? 'updated' : 'created'}. You can now add its teams and stages.`
}
const stage = ref({
  id: 0,
  league_id: 0,
  name: '',
  type: 'round_robin',
  sort_order: 0,
  team_limit: 0,
  expected_teams: 0,
  source_stage_id: null as number | null,
  round: 1,
})
const stageTypeChosen = ref(false)
function defaultStageType(leagueId: number) {
  return w.value?.leagues.find((league) => league.id === leagueId)?.mode === 'scheduling'
    ? 'individual'
    : 'round_robin'
}
watch(
  () => stage.value.league_id,
  (leagueId) => {
    if (!stage.value.id && !stageTypeChosen.value) stage.value.type = defaultStageType(leagueId)
  },
)
const team = ref({
  id: 0,
  league_id: 0,
  name: '',
  affiliation: '',
  startnumber: 1,
  external_key: '',
})
const field = ref({ id: 0, league_id: 0, name: '' })
const referee = ref({ id: 0, name: '', user_id: '', available_from: '', available_until: '' })
const slot = ref({ field_id: 0, starts_at: '', ends_at: '', interval: 30 })
const copy = ref({ source_field_id: 0, target_field_id: 0 })
const event = ref<Partial<Tournament>>({})
const copiedDisplayLink = ref('')
function fieldDisplayPath(fieldId: number) {
  return `/display/${encodeURIComponent(w.value?.tournament.abbrev ?? '')}/fields/${fieldId}`
}
async function copyFieldDisplayLink(fieldId: number) {
  const url = new URL(window.location.href)
  url.hash = fieldDisplayPath(fieldId)
  copiedDisplayLink.value = url.toString()
  try {
    await navigator.clipboard.writeText(copiedDisplayLink.value)
    notice.value = 'Field display link copied.'
  } catch {
    notice.value = 'Select and copy the field display URL below.'
    await nextTick()
    const input = document.getElementById('field-display-url') as HTMLInputElement | null
    input?.focus()
    input?.select()
  }
}
const teamFilter = ref(0)
const filteredTeams = computed(
  () => w.value?.teams.filter((t) => !teamFilter.value || t.league_id === teamFilter.value) ?? [],
)
const stageLeagueChoices = computed(() =>
  leagueChoices(
    stage.value.id
      ? w.value?.stages.find((item) => item.id === stage.value.id)?.league_id
      : undefined,
  ),
)
const teamLeagueChoices = computed(() =>
  leagueChoices(
    team.value.id ? w.value?.teams.find((item) => item.id === team.value.id)?.league_id : undefined,
  ),
)
function leagueChoices(legacyId?: number) {
  const legacy = mainLeagues.value.find((item) => item.id === legacyId)
  return legacy ? [...subLeagues.value, legacy] : subLeagues.value
}
function resetStage() {
  stageTypeChosen.value = false
  const leagueId = subLeagues.value.some((item) => item.id === stage.value.league_id)
    ? stage.value.league_id
    : (subLeagues.value[0]?.id ?? 0)
  stage.value = {
    id: 0,
    league_id: leagueId,
    name: '',
    type: defaultStageType(leagueId),
    sort_order: w.value?.stages.length ?? 0,
    team_limit: 0,
    expected_teams: 0,
    source_stage_id: null,
    round: 1,
  }
}
function resetTeam() {
  const leagueId = subLeagues.value.some((item) => item.id === team.value.league_id)
    ? team.value.league_id
    : (subLeagues.value[0]?.id ?? 0)
  team.value = {
    id: 0,
    league_id: leagueId,
    name: '',
    affiliation: '',
    startnumber: team.value.startnumber + 1,
    external_key: '',
  }
}
function automaticStage(item: { type: string }) {
  return item.type !== 'manual'
}
function stageSlots(id: number) {
  return w.value?.matches.filter((match) => match.stage_id === id && !match.is_bye) ?? []
}
const plannedTeamCount = computed(() => {
  const count =
    stage.value.expected_teams ||
    w.value?.teams.filter((team) => team.league_id === stage.value.league_id).length ||
    0
  return stage.value.team_limit > 0 ? Math.min(count, stage.value.team_limit) : count
})
async function saveStage(event?: SubmitEvent) {
  if (!stage.value.id && !subLeagues.value.some((item) => item.id === stage.value.league_id)) {
    error.value = 'Add a sub-league before creating stages.'
    return
  }
  const populate =
    event?.submitter instanceof HTMLButtonElement && event.submitter.value === 'populate'
  const data = { ...stage.value }
  if (
    !automaticStage(data) &&
    (!Number.isSafeInteger(data.expected_teams) ||
      data.expected_teams < 0 ||
      data.expected_teams > 256 ||
      data.expected_teams === 1)
  ) {
    const previousCount = w.value?.stages.find((item) => item.id === data.id)?.expected_teams ?? 0
    data.expected_teams = previousCount === 1 ? 0 : previousCount
  }
  if (
    data.type !== 'individual' &&
    ((automaticStage(data) && data.expected_teams === 1) || data.team_limit === 1)
  ) {
    error.value =
      'Paired stages need at least two expected or qualifying teams. Use 0 for the full roster, or choose Individual runs for one team.'
    return
  }
  let saved: Stage | undefined
  if (
    !(await perform(async () => {
      saved = (
        data.id
          ? await api.put<Stage>(`/stages/${data.id}`, data)
          : await api.post<Stage>(`/tournaments/${tournamentId.value}/stages`, data)
      ).data
    })) ||
    !saved
  )
    return
  if (populate) {
    await router.push({
      name: 'stage-games',
      params: { id: tournamentId.value },
      query: { stage: saved.id },
    })
    return
  }
  const count = stageSlots(saved.id).length
  notice.value = automaticStage(saved)
    ? `Stage saved. ${count} game slots are ready for scheduling. Populate teams when the roster or source results are ready.`
    : 'Stage saved.'
  resetStage()
}
async function saveTeam() {
  if (!team.value.id && !subLeagues.value.some((item) => item.id === team.value.league_id)) {
    error.value = 'Add a sub-league before registering teams.'
    return
  }
  await save('teams', team.value, resetTeam)
}
watch(w, (value) => {
  if (!value) return
  event.value = { ...value.tournament }
  const subId = subLeagues.value[0]?.id ?? 0
  if (!stage.value.id && !subLeagues.value.some((item) => item.id === stage.value.league_id))
    stage.value.league_id = subId
  if (!team.value.id && !subLeagues.value.some((item) => item.id === team.value.league_id))
    team.value.league_id = subId
  if (!field.value.league_id) field.value.league_id = mainLeagues.value[0]?.id ?? subId
  if (!subLeague.value.parent_id) subLeague.value.parent_id = mainLeagues.value[0]?.id ?? null
  if (!slot.value.field_id) slot.value.field_id = value.fields[0]?.id ?? 0
})
async function save(collection: string, value: { id: number }, reset: () => void) {
  const data = { ...value }
  if (
    await perform(() =>
      value.id
        ? api.put(`/${collection}/${value.id}`, data)
        : api.post(`/tournaments/${tournamentId.value}/${collection}`, data),
    )
  )
    reset()
}
async function remove(collection: string, id: number, name: string) {
  if (!confirm(`Delete ${name}? Existing dependencies may prevent deletion.`)) return
  if (collection === 'tournaments') {
    busy.value = true
    try {
      await api.delete(`/tournaments/${id}`)
      await loadTournaments()
      await router.push('/')
    } catch (e) {
      error.value = errorMessage(e)
    } finally {
      busy.value = false
    }
    return
  }
  await perform(() => api.delete(`/${collection}/${id}`))
}
function editStage(item: Stage) {
  stageTypeChosen.value = true
  stage.value = { ...item, expected_teams: item.expected_teams ?? 0 }
  tab.value = 'stages'
}
function editTeam(item: Team) {
  team.value = { ...item }
  tab.value = 'teams'
}
function editField(item: Field) {
  field.value = { ...item }
  tab.value = 'fields'
}
function editReferee(item: Referee) {
  referee.value = {
    ...item,
    available_from: item.available_from ? toLocal(item.available_from) : '',
    available_until: item.available_until ? toLocal(item.available_until) : '',
  }
  tab.value = 'referees'
}
function toLocal(value: string) {
  const date = new Date(value)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}
async function saveReferee() {
  const item = {
    ...referee.value,
    available_from: referee.value.available_from
      ? new Date(referee.value.available_from).toISOString()
      : null,
    available_until: referee.value.available_until
      ? new Date(referee.value.available_until).toISOString()
      : null,
  }
  await save('referees', item, () => {
    referee.value = { id: 0, name: '', user_id: '', available_from: '', available_until: '' }
  })
}
async function copySlots() {
  await perform(() =>
    api.post(`/fields/${copy.value.target_field_id}/copy-slots`, {
      source_field_id: copy.value.source_field_id,
    }),
  )
}
async function saveEvent() {
  await perform(() => api.put(`/tournaments/${tournamentId.value}`, event.value))
}
async function createSlots() {
  const start = new Date(slot.value.starts_at).getTime(),
    end = new Date(slot.value.ends_at).getTime(),
    interval = slot.value.interval * 60000
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || interval < 60000) {
    error.value = 'Choose a valid availability window and a slot length of at least one minute.'
    return
  }
  if (Math.ceil((end - start) / interval) > 200) {
    error.value = 'Create at most 200 slots at a time.'
    return
  }
  await perform(async () => {
    for (let time = start; time + interval <= end; time += interval)
      await api.post(`/tournaments/${tournamentId.value}/slots`, {
        field_id: slot.value.field_id,
        starts_at: new Date(time).toISOString(),
        ends_at: new Date(time + interval).toISOString(),
      })
  })
}
</script>
<template>
  <div
    class="page-heading flex items-center justify-between gap-[20px] max-[1200px]:items-start max-[760px]:gap-[18px]"
  >
    <div>
      <p class="eyebrow">EVENT CONFIGURATION</p>
      <h1>Tournament setup</h1>
      <p>{{ w?.tournament.name || 'Configure the foundations of your event.' }}</p>
    </div>
    <RouterLink :to="`/tournaments/${tournamentId}/schedule`" class="button"
      >Match schedule →</RouterLink
    >
  </div>
  <div v-if="!canAdmin" class="panel empty">
    <h2>Administrator access required</h2>
    <p>Your role can view the match schedule. An administrator manages tournament configuration.</p>
  </div>
  <template v-else
    ><div class="tabs">
      <button
        v-for="item in tabs"
        :key="item.id"
        :class="{ active: tab === item.id }"
        @click="chooseTab(item.id)"
      >
        {{ item.name }}
      </button>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p
      v-if="notice"
      role="status"
      class="mb-[20px] rounded-[8px] border border-[#d7e8c6] bg-[#f2f8e9] px-[16px] py-[12px] text-[12px] text-[#4d7139]"
    >
      {{ notice }}
    </p>
    <div v-if="loading && !w" class="loading">Loading tournament…</div>
    <template v-if="w">
      <div v-if="tab === 'main-leagues'" class="setup-columns">
        <section class="panel">
          <div class="section-title">
            <h2>Main leagues</h2>
            <span class="badge">{{ mainLeagues.length }} main leagues</span>
          </div>
          <p class="muted">
            Start with a main league such as Soccer or Rescue. Choose how its matches are managed,
            then add its sub-leagues.
          </p>
          <div v-if="!mainLeagues.length" class="empty compact">
            Create your first main league using the form.
          </div>
          <article
            v-for="item in mainLeagues"
            :key="item.id"
            class="mb-[16px] rounded-[9px] border border-[color:var(--border)] p-[16px] last:mb-0"
          >
            <div class="flex flex-wrap items-start justify-between gap-[12px]">
              <div>
                <h3 class="mb-[6px]">{{ item.name }}</h3>
                <p class="hint">
                  {{ item.abbrev }} ·
                  {{ item.mode === 'full' ? 'Full matchmaking' : 'Scheduling only' }} ·
                  {{ item.referee_mode ? 'Referee control enabled' : 'Manual result entry' }}
                </p>
              </div>
              <span class="badge"
                >{{
                  subLeagues.filter((child) => child.parent_id === item.id).length
                }}
                sub-leagues</span
              >
            </div>
            <ul
              v-if="subLeagues.some((child) => child.parent_id === item.id)"
              class="my-[16px] space-y-[8px] border-l-2 border-[#e6eddd] pl-[12px]"
            >
              <li
                v-for="child in subLeagues.filter((child) => child.parent_id === item.id)"
                :key="child.id"
                class="flex items-center justify-between gap-[12px] text-[12px]"
              >
                <span>{{ child.name }}</span
                ><button class="text-button" @click="editSubLeague(child)">Edit sub-league</button>
              </li>
            </ul>
            <p v-else class="hint my-[14px]">
              No sub-leagues yet. Add one before registering teams or creating stages.
            </p>
            <div class="button-group">
              <button class="button small" @click="editMainLeague(item)">Edit main league</button
              ><button class="button small primary" @click="addSubLeague(item.id)">
                ＋ Add sub-league</button
              ><button
                class="text-button danger-text ml-auto"
                :disabled="busy"
                @click="remove('leagues', item.id, item.name)"
              >
                Delete
              </button>
            </div>
          </article>
        </section>
        <form class="panel form-grid" @submit.prevent="saveMainLeague">
          <h2 class="full-width">
            {{ mainLeague.id ? 'Edit main league' : 'Create main league' }}
          </h2>
          <label>Name<input v-model="mainLeague.name" required placeholder="Soccer" /></label>
          <label
            >Abbreviation<input
              v-model="mainLeague.abbrev"
              required
              pattern="[A-Za-z0-9_-]+"
              placeholder="soccer"
          /></label>
          <label class="full-width"
            >Management mode<select v-model="mainLeague.mode">
              <option value="full">Full matchmaking & standings</option>
              <option value="scheduling">Scheduling only</option>
            </select></label
          >
          <label v-if="mainLeague.mode === 'full'" class="checkbox full-width"
            ><input v-model="mainLeague.referee_mode" type="checkbox" /> Enable referee match
            control</label
          >
          <p v-else class="hint full-width">
            Scheduling-only leagues use manual result entry. Referee match control is disabled.
          </p>
          <p class="hint full-width">
            These management settings apply to all sub-leagues. Configure robot inspections and
            match clocks in the Sub-leagues tab.
          </p>
          <div class="form-actions">
            <button class="button primary" :disabled="busy">
              {{ mainLeague.id ? 'Save main league' : 'Create main league & continue' }}</button
            ><button
              v-if="mainLeague.id"
              type="button"
              class="button"
              :disabled="busy"
              @click="cancelMainEdit"
            >
              Cancel edit
            </button>
          </div>
        </form>
      </div>
      <div v-if="tab === 'sub-leagues' && !mainLeagues.length" class="panel empty">
        <span class="empty-icon">⚑</span>
        <h2>Create a main league first</h2>
        <p>
          A sub-league belongs to a main league. Create Soccer, Rescue or another main league before
          adding its sub-leagues.
        </p>
        <button class="button primary" @click="addMainLeague">Create main league →</button>
      </div>
      <div v-else-if="tab === 'sub-leagues'" class="setup-columns">
        <section class="panel">
          <div class="section-title">
            <h2>Sub-leagues</h2>
            <span class="badge">{{ subLeagues.length }} sub-leagues</span>
          </div>
          <p class="muted">
            Each sub-league has its own teams, stages, robot inspections and match clock.
          </p>
          <article v-for="parent in mainLeagues" :key="parent.id" class="mb-[20px] last:mb-0">
            <div
              class="flex items-center justify-between gap-[12px] border-b border-[color:var(--border)] pb-[10px]"
            >
              <strong class="text-[13px]">{{ parent.name }}</strong
              ><button class="text-button" @click="addSubLeague(parent.id)">
                ＋ Add sub-league
              </button>
            </div>
            <div
              v-if="!subLeagues.some((child) => child.parent_id === parent.id)"
              class="empty compact"
            >
              No sub-leagues under {{ parent.name }} yet.
            </div>
            <div
              v-for="item in subLeagues.filter((child) => child.parent_id === parent.id)"
              :key="item.id"
              class="entity-row"
            >
              <div>
                <strong>{{ item.name }}</strong
                ><small
                  >{{ item.abbrev }} · {{ item.robot_count }} robots / team ·
                  {{ item.inspections_enabled === false ? 'Inspections off' : 'Inspections on' }} ·
                  {{
                    item.referee_mode ? `${item.half_seconds}s halves` : 'Match clock off'
                  }}</small
                >
              </div>
              <div class="button-group">
                <button class="text-button" @click="editSubLeague(item)">Edit sub-league</button
                ><button
                  class="text-button danger-text"
                  :disabled="busy"
                  @click="remove('leagues', item.id, item.name)"
                >
                  Delete
                </button>
              </div>
            </div>
          </article>
          <div
            v-if="subLeagues.length"
            class="mt-[20px] flex flex-wrap gap-[10px] border-t border-[color:var(--border)] pt-[18px]"
          >
            <button class="button" @click="chooseTab('teams')">Add teams →</button
            ><button class="button" @click="chooseTab('stages')">Set up stages →</button>
          </div>
        </section>
        <form class="panel form-grid" @submit.prevent="saveSubLeague">
          <h2 class="full-width">{{ subLeague.id ? 'Edit sub-league' : 'Add sub-league' }}</h2>
          <label class="full-width"
            >Main league<select v-model="subLeague.parent_id" required>
              <option :value="null" disabled>Select an existing main league</option>
              <option v-for="parent in mainLeagues" :key="parent.id" :value="parent.id">
                {{ parent.name }}
              </option>
            </select></label
          >
          <label
            >Name<input v-model="subLeague.name" required placeholder="Soccer Infrared" /></label
          ><label
            >Abbreviation<input
              v-model="subLeague.abbrev"
              required
              pattern="[A-Za-z0-9_-]+"
              placeholder="infrared"
          /></label>
          <div v-if="parentLeague" class="full-width rounded-[8px] bg-[#f5f8ee] p-[14px]">
            <p class="hint mb-[8px]">
              Inherited from <strong>{{ parentLeague.name }}</strong
              >:
              {{
                parentLeague.mode === 'full' ? 'full matchmaking & standings' : 'scheduling only'
              }}
              · {{ parentLeague.referee_mode ? 'referee control enabled' : 'manual result entry' }}.
            </p>
            <button type="button" class="text-button" @click="editMainLeague(parentLeague, true)">
              Edit main league settings →
            </button>
          </div>
          <h3 id="inspection-settings-title" class="full-width form-section">Robot inspection</h3>
          <label class="checkbox full-width"
            ><input
              v-model="subLeague.inspections_enabled"
              type="checkbox"
              aria-controls="inspection-limits"
            />
            Enable robot inspections</label
          >
          <label
            >Robots per team<input
              v-model.number="subLeague.robot_count"
              type="number"
              min="1"
              max="20"
              required
            /><small>Used for the timeout roster, including when inspections are off.</small></label
          >
          <p v-if="!subLeague.inspections_enabled" class="hint full-width">
            Pregame robot inspections are skipped for this sub-league. Saved inspection limits are
            retained.
          </p>
          <fieldset
            id="inspection-limits"
            class="full-width form-grid min-w-0 border-0 p-0 disabled:opacity-50"
            :disabled="!subLeague.inspections_enabled"
            aria-labelledby="inspection-settings-title"
          >
            <label
              >Maximum weight (g)<input
                v-model.number="subLeague.max_weight_g"
                type="number"
                min="0"
                :required="subLeague.inspections_enabled"
              /><small>0 = unlimited</small></label
            >
            <label
              >Ball capturing zone (cm)<input
                v-model.number="subLeague.ball_zone_cm"
                type="number"
                min="0"
                step="0.1"
                :required="subLeague.inspections_enabled"
            /></label>
            <label
              >Maximum voltage (V)<input
                v-model.number="subLeague.voltage"
                type="number"
                min="0"
                step="0.1"
                :required="subLeague.inspections_enabled"
            /></label>
            <label
              >Maximum size (cm)<input
                v-model.number="subLeague.size_cm"
                type="number"
                min="0"
                step="0.1"
                :required="subLeague.inspections_enabled"
            /></label>
            <label
              >Maximum motors<input
                v-model.number="subLeague.motor_limit"
                type="number"
                min="0"
                :required="subLeague.inspections_enabled"
            /></label>
          </fieldset>
          <h3 id="match-clock-settings-title" class="full-width form-section">Match clock</h3>
          <p v-if="!parentLeague?.referee_mode" class="hint full-width">
            Enable referee control on the main league to configure the match clock. Saved durations
            are retained.
          </p>
          <fieldset
            class="full-width form-grid min-w-0 border-0 p-0 disabled:opacity-50"
            :disabled="!parentLeague?.referee_mode"
            aria-labelledby="match-clock-settings-title"
          >
            <label
              >Each half (seconds)<input
                v-model.number="subLeague.half_seconds"
                type="number"
                min="1"
                :required="Boolean(parentLeague?.referee_mode)"
            /></label>
            <label
              >Halftime (seconds)<input
                v-model.number="subLeague.halftime_seconds"
                type="number"
                min="1"
                :required="Boolean(parentLeague?.referee_mode)"
            /></label>
          </fieldset>
          <p class="hint full-width">
            Inspection limits and clock durations apply only to this sub-league. Adjust the starting
            values to your event's rules.
          </p>
          <div class="form-actions">
            <button class="button primary" :disabled="busy || !parentLeague">
              {{ subLeague.id ? 'Save sub-league' : 'Add sub-league' }}</button
            ><button
              v-if="subLeague.id"
              class="button"
              type="button"
              :disabled="busy"
              @click="cancelSubEdit"
            >
              Cancel edit
            </button>
          </div>
        </form>
      </div>
      <div v-if="tab === 'stages'" class="setup-columns">
        <section class="panel">
          <h2>Competition stages</h2>
          <p class="muted">Generate pairings from the schedule after adding teams.</p>
          <div v-if="!w.stages.length" class="empty compact">No stages yet.</div>
          <article v-for="item in w.stages" :key="item.id" class="entity-row">
            <div>
              <strong>{{ item.name }}</strong
              ><small
                >{{ leagueName(item.league_id) }} · {{ item.type.replace('_', ' ') }} · Order
                {{ item.sort_order }}</small
              ><small v-if="automaticStage(item)"
                >{{ stageSlots(item.id).length }} game slots ·
                {{ stageSlots(item.id).filter((match) => match.is_placeholder).length }} awaiting
                teams</small
              >
            </div>
            <div class="button-group flex flex-wrap items-center gap-[9px]">
              <RouterLink
                :to="{
                  name: 'stage-games',
                  params: { id: tournamentId },
                  query: { stage: item.id },
                }"
                class="text-button"
                >{{ automaticStage(item) ? 'Populate teams →' : 'Stage games →' }}</RouterLink
              >
              <button class="text-button" @click="editStage(item)">Edit</button
              ><button
                class="text-button danger-text"
                @click="remove('stages', item.id, item.name)"
              >
                Delete
              </button>
            </div>
          </article>
        </section>
        <section v-if="!subLeagues.length && !stage.id" class="panel empty">
          <h2>Add a sub-league first</h2>
          <p>Stages belong to sub-leagues. Existing stages remain listed here.</p>
          <button
            class="button primary"
            @click="mainLeagues.length ? addSubLeague() : addMainLeague()"
          >
            {{ mainLeagues.length ? 'Add sub-league →' : 'Create main league first →' }}
          </button>
        </section>
        <form v-else class="panel form-grid" @submit.prevent="saveStage">
          <h2 class="full-width">{{ stage.id ? 'Edit stage' : 'Add a stage' }}</h2>
          <label
            >Sub-league<select v-model="stage.league_id" required>
              <option :value="0" disabled>Select league</option>
              <option v-for="l in stageLeagueChoices" :key="l.id" :value="l.id">
                {{ l.name }}{{ l.parent_id ? '' : ' (existing main-league stage)' }}
              </option>
            </select></label
          ><label>Name<input v-model="stage.name" required placeholder="Round robin 1" /></label
          ><label
            >Pairing system<select v-model="stage.type" @change="stageTypeChosen = true">
              <option value="round_robin">Round robin</option>
              <option value="swiss">Swiss system</option>
              <option value="knockout">Knockout</option>
              <option value="individual">Individual runs</option>
              <option value="manual">Manual games</option>
            </select></label
          ><label
            >Sort order<input
              v-model.number="stage.sort_order"
              type="number"
              min="0"
              required /></label
          ><label
            >Qualifying teams<input
              v-model.number="stage.team_limit"
              type="number"
              min="0"
              required
            /><small>{{
              stage.type === 'individual'
                ? '0 = all teams; 1 is allowed for a single-team run.'
                : '0 = all teams; otherwise at least 2. Set 4 for semifinals or 2 for a final.'
            }}</small></label
          ><label v-if="automaticStage(stage)"
            >Expected teams<input
              v-model.number="stage.expected_teams"
              type="number"
              min="0"
              max="256"
              step="1"
              required
            /><small
              >0 = use the registered roster.
              {{
                stage.type === 'individual'
                  ? 'Set 1 or more to reserve one run per expected team.'
                  : 'Set 2 or more to reserve paired games before teams are added.'
              }}
              The qualifying-team limit still applies.</small
            ></label
          ><label class="full-width"
            >Seed from stage<select v-model="stage.source_stage_id">
              <option :value="null">Current league standings / registration order</option>
              <option
                v-for="s in w.stages.filter(
                  (s) => s.league_id === stage.league_id && s.id !== stage.id,
                )"
                :key="s.id"
                :value="s.id"
              >
                {{ s.name }}
              </option>
            </select></label
          >
          <div v-if="automaticStage(stage)" class="full-width rounded-[8px] bg-[#f5f8ee] p-[16px]">
            <h3>Automatic game slots</h3>
            <p class="hint">
              {{
                plannedTeamCount
                  ? `Plan for ${plannedTeamCount} ${plannedTeamCount === 1 ? 'team' : 'teams'}.`
                  : 'Set the expected team count to reserve games before registration.'
              }}
              {{
                stage.type === 'individual'
                  ? 'One individual run is reserved for each team.'
                  : stage.type === 'round_robin'
                    ? 'All round-robin fixtures are reserved.'
                    : 'This stage contains one round. Create a separate stage seeded from this one for the next round.'
              }}
              Saving prepares empty games that can already receive fields, times and referees.
            </p>
            <template v-if="stage.id"
              ><p class="hint mt-[12px]">
                {{ stageSlots(stage.id).length }} saved game slots ·
                {{ stageSlots(stage.id).filter((match) => match.is_placeholder).length }} awaiting
                teams. Populate teams after the roster or source-stage results are ready;
                reservations stay attached.
              </p>
              <button
                type="submit"
                name="stage-action"
                value="populate"
                class="button mt-[14px]"
                :disabled="busy"
              >
                Save & populate teams →
              </button></template
            >
          </div>
          <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
            <button class="button primary" :disabled="busy">Save stage</button
            ><button v-if="stage.id" class="button" type="button" @click="resetStage">
              Cancel edit
            </button>
          </div>
        </form>
      </div>
      <div v-if="tab === 'teams'" class="setup-columns">
        <section class="panel">
          <div class="section-title flex items-center justify-between gap-[12px]">
            <h2>Registered teams</h2>
            <span class="badge">{{ filteredTeams.length }} teams</span>
          </div>
          <label
            >Filter league<select v-model="teamFilter">
              <option :value="0">All leagues</option>
              <option v-for="l in w.leagues" :key="l.id" :value="l.id">{{ l.name }}</option>
            </select></label
          >
          <div v-if="!filteredTeams.length" class="empty compact">No teams registered.</div>
          <article v-for="item in filteredTeams" :key="item.id" class="entity-row">
            <div>
              <strong
                ><span class="muted">#{{ item.startnumber }}</span> {{ item.name }}</strong
              ><small
                >{{ leagueName(item.league_id) }} ·
                {{ item.affiliation || 'No affiliation' }}</small
              >
            </div>
            <div class="button-group flex flex-wrap items-center gap-[9px]">
              <button class="text-button" @click="editTeam(item)">Edit</button
              ><button class="text-button danger-text" @click="remove('teams', item.id, item.name)">
                Delete
              </button>
            </div>
          </article>
        </section>
        <section v-if="!subLeagues.length && !team.id" class="panel empty">
          <h2>Add a sub-league first</h2>
          <p>Register teams under a sub-league. Existing teams remain listed here.</p>
          <button
            class="button primary"
            @click="mainLeagues.length ? addSubLeague() : addMainLeague()"
          >
            {{ mainLeagues.length ? 'Add sub-league →' : 'Create main league first →' }}
          </button>
        </section>
        <form v-else class="panel form-grid" @submit.prevent="saveTeam">
          <h2 class="full-width">{{ team.id ? 'Edit team' : 'Add a team' }}</h2>
          <label class="full-width"
            >Team name<input v-model="team.name" required placeholder="Bodensee Koalas" /></label
          ><label
            >Sub-league<select v-model="team.league_id" required>
              <option :value="0" disabled>Select league</option>
              <option v-for="l in teamLeagueChoices" :key="l.id" :value="l.id">
                {{ l.name }}{{ l.parent_id ? '' : ' (existing main-league team)' }}
              </option>
            </select></label
          ><label
            >Start number<input
              v-model.number="team.startnumber"
              type="number"
              min="1"
              required /></label
          ><label class="full-width"
            >Affiliation / school / country<input v-model="team.affiliation" /></label
          ><label class="full-width"
            >External integration key<input v-model="team.external_key"
          /></label>
          <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
            <button class="button primary" :disabled="busy">Save team</button
            ><button v-if="team.id" type="button" class="button" @click="resetTeam">
              Cancel edit
            </button>
          </div>
        </form>
      </div>
      <div v-if="tab === 'fields'" class="setup-columns">
        <section class="panel">
          <h2>Fields</h2>
          <label v-if="copiedDisplayLink" class="mb-[18px]"
            >Field display URL<input id="field-display-url" :value="copiedDisplayLink" readonly
          /></label>
          <div v-if="!w.fields.length" class="empty compact">
            Add a field, then create its available timeslots.
          </div>
          <article v-for="item in w.fields" :key="item.id" class="entity-row">
            <div>
              <strong>{{ item.name }}</strong
              ><small
                >{{ leagueName(item.league_id) }} ·
                {{ w.slots.filter((s) => s.field_id === item.id).length }} slots</small
              >
              <div class="mt-[8px] flex flex-wrap gap-[12px]">
                <RouterLink
                  :to="fieldDisplayPath(item.id)"
                  target="_blank"
                  rel="noopener"
                  class="text-button"
                  >Open field display ↗</RouterLink
                ><button type="button" class="text-button" @click="copyFieldDisplayLink(item.id)">
                  Copy display link
                </button>
              </div>
            </div>
            <div class="button-group flex flex-wrap items-center gap-[9px]">
              <button class="text-button" @click="editField(item)">Edit</button
              ><button
                class="text-button danger-text"
                @click="remove('fields', item.id, item.name)"
              >
                Delete
              </button>
            </div>
          </article>
          <form
            class="form-grid inset-form"
            @submit.prevent="
              save('fields', field, () => (field = { id: 0, league_id: field.league_id, name: '' }))
            "
          >
            <h3 class="full-width">{{ field.id ? 'Edit field' : 'Add field' }}</h3>
            <label>Field name<input v-model="field.name" required placeholder="Soccer A" /></label
            ><label
              >League<select v-model="field.league_id" required>
                <option :value="0" disabled>Select league</option>
                <option v-for="l in w.leagues" :key="l.id" :value="l.id">{{ l.name }}</option>
              </select></label
            ><button class="button primary" :disabled="busy">Save field</button>
          </form>
        </section>
        <div class="stack">
          <form class="panel form-grid" @submit.prevent="createSlots">
            <h2 class="full-width">Create availability</h2>
            <p class="muted full-width">
              Times below use your device timezone ({{
                Intl.DateTimeFormat().resolvedOptions().timeZone
              }}). They are displayed in the tournament timezone on the schedule.
            </p>
            <label class="full-width"
              >Field<select v-model="slot.field_id" required>
                <option :value="0" disabled>Select field</option>
                <option v-for="f in w.fields" :key="f.id" :value="f.id">{{ f.name }}</option>
              </select></label
            ><label
              >Available from<input
                v-model="slot.starts_at"
                type="datetime-local"
                required /></label
            ><label
              >Available until<input v-model="slot.ends_at" type="datetime-local" required /></label
            ><label
              >Slot length (minutes)<input
                v-model.number="slot.interval"
                type="number"
                min="1"
                max="1440"
                required
            /></label>
            <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
              <button class="button primary" :disabled="busy || !slot.field_id">
                Create timeslots
              </button>
            </div>
          </form>
          <form class="panel form-grid" @submit.prevent="copySlots">
            <h2 class="full-width">Copy field availability</h2>
            <label
              >Copy from<select v-model="copy.source_field_id" required>
                <option :value="0" disabled>Source field</option>
                <option v-for="f in w.fields" :key="f.id" :value="f.id">{{ f.name }}</option>
              </select></label
            ><label
              >Copy to<select v-model="copy.target_field_id" required>
                <option :value="0" disabled>Target field</option>
                <option
                  v-for="f in w.fields.filter((f) => f.id !== copy.source_field_id)"
                  :key="f.id"
                  :value="f.id"
                >
                  {{ f.name }}
                </option>
              </select></label
            ><button
              class="button"
              :disabled="busy || !copy.source_field_id || !copy.target_field_id"
            >
              Copy timeslots
            </button>
          </form>
          <section class="panel">
            <h3>Existing availability</h3>
            <p v-if="!w.slots.length" class="muted">No timeslots yet.</p>
            <div class="slot-list">
              <div v-for="s in w.slots" :key="s.id" class="entity-row">
                <div>
                  <strong>{{ w.fields.find((f) => f.id === s.field_id)?.name }}</strong
                  ><small
                    >{{ formatDate(s.starts_at, w.tournament.timezone) }} –
                    {{
                      formatDate(s.ends_at, w.tournament.timezone, {
                        month: undefined,
                        day: undefined,
                      })
                    }}</small
                  >
                </div>
                <button
                  class="text-button danger-text"
                  :disabled="busy"
                  @click="remove('slots', s.id, 'this timeslot')"
                >
                  Delete
                </button>
              </div>
            </div>
          </section>
        </div>
      </div>
      <div v-if="tab === 'referees'" class="setup-columns">
        <section class="panel">
          <h2>Referee roster</h2>
          <div v-if="!w.referees.length" class="empty compact">
            Add referees to assign them to games.
          </div>
          <article v-for="item in w.referees" :key="item.id" class="entity-row">
            <div>
              <strong>{{ item.name }}</strong
              ><small
                >{{
                  item.available_from
                    ? formatDate(item.available_from, w.tournament.timezone)
                    : 'No start restriction'
                }}
                →
                {{
                  item.available_until
                    ? formatDate(item.available_until, w.tournament.timezone)
                    : 'No end restriction'
                }}</small
              >
            </div>
            <div class="button-group flex flex-wrap items-center gap-[9px]">
              <button class="text-button" @click="editReferee(item)">Edit</button
              ><button
                class="text-button danger-text"
                @click="remove('referees', item.id, item.name)"
              >
                Delete
              </button>
            </div>
          </article>
        </section>
        <form class="panel form-grid" @submit.prevent="saveReferee">
          <h2 class="full-width">{{ referee.id ? 'Edit referee' : 'Add a referee' }}</h2>
          <label class="full-width">Full name<input v-model="referee.name" required /></label
          ><label class="full-width"
            >FusionAuth user ID<input v-model="referee.user_id" placeholder="User UUID" /><small
              >Links this referee to their sign-in account.</small
            ></label
          ><label
            >Available from<input v-model="referee.available_from" type="datetime-local" /></label
          ><label
            >Available until<input v-model="referee.available_until" type="datetime-local"
          /></label>
          <p class="hint full-width">
            Leave bounds empty for unrestricted availability. Enter times in your device timezone.
          </p>
          <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
            <button class="button primary" :disabled="busy">Save referee</button
            ><button v-if="referee.id" type="button" class="button" @click="referee.id = 0">
              Save as new instead
            </button>
          </div>
        </form>
      </div>
      <form v-if="tab === 'event'" class="panel form-grid event-form" @submit.prevent="saveEvent">
        <h2 class="full-width">Event details</h2>
        <label>Tournament name<input v-model="event.name" required /></label
        ><label
          >Public abbreviation<input
            v-model="event.abbrev"
            required
            pattern="[A-Za-z0-9_-]+" /></label
        ><label>First day<input v-model="event.start_date" type="date" required /></label
        ><label
          >Last day<input
            v-model="event.end_date"
            type="date"
            :min="event.start_date"
            required /></label
        ><label class="full-width"
          >IANA timezone<input v-model="event.timezone" required placeholder="Europe/Berlin"
        /></label>
        <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
          <button class="button primary" :disabled="busy">Save event</button
          ><button
            type="button"
            class="button danger"
            @click="remove('tournaments', tournamentId, w.tournament.name)"
          >
            Delete tournament
          </button>
        </div>
      </form>
    </template></template
  >
</template>
