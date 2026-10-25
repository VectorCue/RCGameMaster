<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import CompetitionSelector from '@/components/CompetitionSelector.vue'
import { isAxiosError } from 'axios'
import { useWorkspace } from '@/composables/workspace'
import { api, canAdmin, errorMessage, formatDate } from '@/lib/api'
import type { Match, Stage } from '@/lib/types'

interface PairingPreview {
  matches: Match[]
  preview_token: string
  round: number
}
interface ManualDraft {
  key: number
  team1_id: number
  team2_id: number | null
  duration_minutes: number
  round: number
  notes: string
}
const route = useRoute()
const router = useRouter()
const selectionOpen = ref(true)
const {
  workspace: w,
  tournamentId,
  loading,
  error,
  busy,
  perform,
  reload,
  teamName,
  leagueName,
  stageName,
} = useWorkspace()
const leagueFilter = ref(0),
  stageId = ref(0),
  round = ref(1)
const clearDialog = ref<HTMLDialogElement | null>(null)
const clearTarget = ref<Stage | null>(null)
const clearScope = ref<'all' | 'latest'>('all')
const clearing = ref(false),
  clearError = ref('')
const clearTargetMatches = computed(
  () => w.value?.matches.filter((match) => match.stage_id === clearTarget.value?.id) ?? [],
)
const clearResetsAssignments = computed(() =>
  Boolean(clearTarget.value && automaticStage(clearTarget.value)),
)
const clearAffectedMatches = computed(() =>
  clearResetsAssignments.value
    ? clearTargetMatches.value.filter((match) => !match.is_placeholder)
    : clearTargetMatches.value,
)
const clearRounds = computed(() =>
  [...new Set(clearAffectedMatches.value.map((match) => match.round))].sort((a, b) => a - b),
)
const latestClearRound = computed(() => clearRounds.value[clearRounds.value.length - 1] ?? 0)
const allowLatestClear = computed(
  () =>
    clearTarget.value &&
    ['swiss', 'knockout', 'manual'].includes(clearTarget.value.type) &&
    clearRounds.value.length > 1,
)
const clearCount = computed(
  () =>
    clearAffectedMatches.value.filter(
      (match) => clearScope.value === 'all' || match.round === latestClearRound.value,
    ).length,
)
const creationMode = ref<'automatic' | 'manual'>('automatic')
const planning = ref(false),
  preparing = ref(false),
  notice = ref(''),
  stalePreview = ref(false)
const preview = ref<PairingPreview | null>(null),
  drafts = ref<ManualDraft[]>([])
let nextDraftKey = 1
const selectedStage = computed(() => w.value?.stages.find((stage) => stage.id === stageId.value))
const selectedLeague = computed(() =>
  w.value?.leagues.find((league) => league.id === selectedStage.value?.league_id),
)
const stages = computed(
  () =>
    w.value?.stages.filter(
      (stage) =>
        !leagueFilter.value ||
        stage.league_id === leagueFilter.value ||
        w.value?.leagues.find((league) => league.id === stage.league_id)?.parent_id ===
          leagueFilter.value,
    ) ?? [],
)
const stageTeams = computed(
  () => w.value?.teams.filter((team) => team.league_id === selectedStage.value?.league_id) ?? [],
)
const existingMatches = computed(
  () =>
    w.value?.matches
      .filter((match) => match.stage_id === stageId.value)
      .sort((a, b) => a.round - b.round || a.number - b.number) ?? [],
)
const automaticAvailable = computed(
  () => selectedStage.value && selectedStage.value.type !== 'manual',
)
const awaitingMatches = computed(() =>
  existingMatches.value.filter((match) => match.is_placeholder),
)
const assignmentsComplete = computed(
  () =>
    automaticAvailable.value && existingMatches.value.length > 0 && !awaitingMatches.value.length,
)
const partialAssignments = computed(
  () =>
    automaticAvailable.value &&
    awaitingMatches.value.length > 0 &&
    existingMatches.value.some((match) => !match.is_placeholder),
)
const working = computed(() => busy.value || planning.value || preparing.value || clearing.value)
function automaticStage(stage: Stage) {
  return stage.type !== 'manual'
}
const defaultDuration = computed(() =>
  selectedLeague.value?.referee_mode
    ? Math.ceil(
        (selectedLeague.value.half_seconds * 2 + selectedLeague.value.halftime_seconds) / 60,
      )
    : 30,
)
function nextRound(stage: Stage) {
  if (automaticStage(stage) || stage.type === 'round_robin' || stage.type === 'manual') return 1
  return (
    Math.max(
      0,
      ...(w.value?.matches
        .filter((match) => match.stage_id === stage.id && !match.is_placeholder)
        .map((match) => match.round) ?? []),
    ) + 1
  )
}
function invalidateDraft() {
  preview.value = null
  drafts.value = []
  stalePreview.value = false
  notice.value = ''
  error.value = ''
}
watch(stageId, () => {
  invalidateDraft()
  if (selectedStage.value)
    round.value = automaticAvailable.value ? 1 : nextRound(selectedStage.value)
  creationMode.value = automaticAvailable.value ? 'automatic' : 'manual'
})
watch([round, creationMode], invalidateDraft)
function chooseStage(stage: Stage) {
  stageId.value = stage.id
  leagueFilter.value = stage.league_id
  selectionOpen.value = false
  void router.replace({ query: { ...route.query, stage: String(stage.id) } })
}
function reopenSelection() {
  leagueFilter.value = selectedStage.value?.league_id ?? 0
  selectionOpen.value = true
}
function cancelSelection() {
  leagueFilter.value = selectedStage.value?.league_id ?? 0
  selectionOpen.value = false
}
function selectRequestedStage() {
  const requested = Number(route.query.stage)
  const stage = w.value?.stages.find((item) => item.id === requested)
  if (stage) {
    leagueFilter.value = stage.league_id
    stageId.value = stage.id
    selectionOpen.value = false
  } else {
    selectionOpen.value = true
    leagueFilter.value = 0
  }
}
watch(() => route.query.stage, selectRequestedStage)
watch(w, (workspace) => {
  if (!workspace) return
  if (!workspace.stages.some((stage) => stage.id === stageId.value)) {
    selectRequestedStage()
    if (!stageId.value || !workspace.stages.some((stage) => stage.id === stageId.value))
      stageId.value = stages.value[0]?.id ?? 0
  }
})
function opponentName(match: Match) {
  if (match.is_placeholder) return 'Awaiting teams'
  return match.team2_id ? teamName(match.team2_id) : match.is_bye ? 'Bye' : 'Individual run'
}
function fieldName(match: Match) {
  return w.value?.fields.find((field) => field.id === match.field_id)?.name ?? 'Unassigned'
}
async function prepareSlots() {
  if (!selectedStage.value || !automaticAvailable.value || !canAdmin.value) return
  const requestedStage = stageId.value
  preparing.value = true
  invalidateDraft()
  try {
    const result = (
      await api.post<{ matches: Match[]; stage: Stage }>(`/stages/${requestedStage}/prepare`, {})
    ).data
    await reload()
    if (!error.value)
      notice.value = `${result.matches.filter((match) => !match.is_bye).length} game slots prepared. Assign fields, times and referees now; populate teams when ready.`
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    preparing.value = false
  }
}
async function previewPairings() {
  if (!selectedStage.value || !canAdmin.value) return
  planning.value = true
  error.value = ''
  notice.value = ''
  preview.value = null
  stalePreview.value = false
  try {
    const requestedStage = stageId.value,
      requestedRound = 1
    // Synchronize saved empty slots before reviewing assignments; bookings are retained.
    await api.post(`/stages/${requestedStage}/prepare`, {})
    await reload()
    if (error.value) return
    await nextTick()
    if (
      stageId.value !== requestedStage ||
      round.value !== requestedRound ||
      creationMode.value !== 'automatic'
    )
      return
    if (partialAssignments.value) {
      notice.value =
        'Some game slots already have teams. Clear the draw before reviewing a complete automatic assignment; scheduled reservations will remain.'
      return
    }
    if (assignmentsComplete.value) {
      notice.value =
        'All teams are already assigned. Clear the draw before repopulating this stage.'
      return
    }
    const response = await api.post<PairingPreview>(`/stages/${requestedStage}/preview`, {
      round: requestedRound,
    })
    if (
      stageId.value === requestedStage &&
      round.value === requestedRound &&
      creationMode.value === 'automatic'
    )
      preview.value = response.data
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    planning.value = false
  }
}
async function submitPairings() {
  const plan = preview.value
  if (!plan || !plan.matches.length || !canAdmin.value) return
  let stale = false
  const submitted = await perform(async () => {
    try {
      await api.post(`/stages/${stageId.value}/generate`, {
        round: plan.round,
        preview_token: plan.preview_token,
      })
    } catch (cause) {
      stale = isAxiosError(cause) && cause.response?.status === 409
      throw cause
    }
  })
  if (submitted) {
    preview.value = null
    round.value = 1
    await nextTick()
    notice.value = `Teams populated for ${plan.matches.length} matches in ${selectedStage.value?.name ?? 'the stage'}. Existing fields, times and referee assignments are retained.`
  } else if (stale) {
    preview.value = null
    stalePreview.value = true
    notice.value =
      'The stage changed after this preview. Preview the pairings again before submitting.'
  }
}
async function openClearDialog() {
  if (!selectedStage.value || !canAdmin.value || !existingMatches.value.length) return
  clearTarget.value = { ...selectedStage.value }
  clearScope.value = 'all'
  clearError.value = ''
  await nextTick()
  clearDialog.value?.showModal()
}
function closeClearDialog() {
  if (clearing.value) return
  clearDialog.value?.close()
  clearTarget.value = null
  clearError.value = ''
}
function cancelClearDialog(event: Event) {
  if (clearing.value) event.preventDefault()
  else {
    clearTarget.value = null
    clearError.value = ''
  }
}
async function clearDraw() {
  const target = clearTarget.value
  if (!target || !canAdmin.value || !clearCount.value || clearing.value) return
  const requestedRound = clearScope.value === 'latest' ? latestClearRound.value : undefined
  const resettingAssignments = clearResetsAssignments.value
  clearing.value = true
  clearError.value = ''
  try {
    // Automatic draws reset teams in booked slots; manual draws remove match rows.
    const result = (
      await api.post<{ cleared: number; round: number; stage: Stage }>(
        `/stages/${target.id}/clear`,
        requestedRound === undefined ? {} : { round: requestedRound },
      )
    ).data
    invalidateDraft()
    if (w.value && !resettingAssignments)
      w.value.matches = w.value.matches.filter(
        (match) =>
          match.stage_id !== target.id ||
          (requestedRound !== undefined && match.round !== requestedRound),
      )
    await reload()
    await nextTick()
    if (selectedStage.value?.id === target.id)
      round.value = automaticAvailable.value ? 1 : nextRound(selectedStage.value)
    await nextTick()
    notice.value = resettingAssignments
      ? `Draw cleared. Team assignments reset for ${result.cleared} games in ${target.name}. Reserved fields, times and referees are retained; preview teams to repopulate.`
      : `Draw cleared. ${result.cleared} matches removed from ${target.name}. Draft new matches to redraw the stage.`
    clearDialog.value?.close()
    clearTarget.value = null
  } catch (cause) {
    clearError.value = errorMessage(cause)
  } finally {
    clearing.value = false
  }
}
function addDraft() {
  if (
    !selectedStage.value ||
    drafts.value.length >= 256 ||
    !Number.isSafeInteger(round.value) ||
    round.value < 1
  )
    return
  notice.value = ''
  drafts.value.push({
    key: nextDraftKey++,
    team1_id: 0,
    team2_id: null,
    duration_minutes: defaultDuration.value,
    round: round.value,
    notes: '',
  })
}
function discardDraft() {
  invalidateDraft()
  notice.value = 'Draft discarded.'
}
function validDraft(draft: ManualDraft) {
  return (
    stageTeams.value.some((team) => team.id === draft.team1_id) &&
    (draft.team2_id === null
      ? selectedLeague.value?.mode === 'scheduling'
      : stageTeams.value.some((team) => team.id === draft.team2_id) &&
        draft.team2_id !== draft.team1_id) &&
    Number.isSafeInteger(draft.duration_minutes) &&
    draft.duration_minutes >= 1 &&
    draft.duration_minutes <= 1440 &&
    Number.isSafeInteger(draft.round) &&
    draft.round >= 1 &&
    draft.notes.length <= 4000
  )
}
async function submitManual() {
  if (!drafts.value.length || !canAdmin.value) return
  if (drafts.value.some((draft) => !validDraft(draft))) {
    error.value =
      'Select valid teams and enter a whole-number duration and round for every draft match.'
    return
  }
  const matches = drafts.value.map((draft) => ({
    team1_id: draft.team1_id,
    team2_id: draft.team2_id,
    duration_minutes: draft.duration_minutes,
    round: draft.round,
    notes: draft.notes,
  }))
  if (await perform(() => api.post(`/stages/${stageId.value}/matches`, { matches }))) {
    drafts.value = []
    notice.value = `${matches.length} matches submitted. Assign their fields and times in Match schedule.`
  }
}
</script>
<template>
  <div
    class="page-heading flex items-center justify-between gap-[20px] max-[1200px]:items-start max-[760px]:gap-[18px]"
  >
    <div>
      <p class="eyebrow">PAIRINGS & STAGE GAMES</p>
      <h1>Stage games</h1>
      <p>Review your pairings and draft matches, then submit them to the stage.</p>
    </div>
    <RouterLink :to="`/tournaments/${tournamentId}/schedule`" class="button"
      >Match schedule →</RouterLink
    >
  </div>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p
    v-if="notice"
    role="status"
    class="mb-[20px] rounded-[8px] border border-[#d7e8c6] bg-[#f2f8e9] px-[16px] py-[12px] text-[12px] text-[#4d7139]"
  >
    {{ notice }}
  </p>
  <div v-if="loading && !w" class="loading">Loading stage games…</div>
  <template v-if="w">
    <section v-if="!w.stages.length" class="panel empty">
      <span class="empty-icon">✣</span>
      <h2>Add a competition stage first</h2>
      <p>Configure a sub-league, register its teams and add its stages before preparing matches.</p>
      <RouterLink v-if="canAdmin" :to="`/tournaments/${tournamentId}/setup`" class="button primary"
        >Tournament setup →</RouterLink
      >
    </section>
    <template v-else>
      <section v-if="selectionOpen" class="panel">
        <div class="section-title">
          <div>
            <h2>Choose league and stage</h2>
            <p>Select a league, then open the stage whose draw you want to manage.</p>
          </div>
          <button
            v-if="selectedStage && route.query.stage"
            type="button"
            class="button small"
            :disabled="working"
            @click="cancelSelection"
          >
            Back to stage
          </button>
        </div>
        <CompetitionSelector
          v-model:league-id="leagueFilter"
          :leagues="w.leagues"
          :stages="w.stages"
          :stage-id="stageId"
          :disabled="working"
          @select-stage="chooseStage"
        />
        <p class="hint mt-[18px]">
          Choosing a different stage clears the current unsubmitted draft.
        </p>
      </section>
      <section v-else-if="selectedStage" class="panel">
        <div class="flex flex-wrap items-center justify-between gap-[16px]">
          <div>
            <p class="eyebrow mb-[8px]">SELECTED COMPETITION</p>
            <h2 class="mb-0">
              {{ leagueName(selectedStage.league_id) }} <span class="mx-[6px] text-muted">/</span>
              {{ selectedStage.name }}
            </h2>
          </div>
          <button type="button" class="button" :disabled="working" @click="reopenSelection">
            Change league / stage
          </button>
        </div>
      </section>
      <template v-if="selectedStage && !selectionOpen">
        <section v-if="canAdmin" class="panel">
          <div class="section-title">
            <div>
              <h2>Prepare matches · {{ selectedStage.name }}</h2>
              <p>
                {{ leagueName(selectedStage.league_id) }} · {{ selectedStage.type.replace('_', ' ')
                }}<span v-if="selectedStage.source_stage_id">
                  · Seeded from {{ stageName(selectedStage.source_stage_id) }}</span
                >
              </p>
            </div>
            <span class="badge">{{ stageTeams.length }} teams</span>
          </div>
          <div class="tabs">
            <button
              :class="{ active: creationMode === 'automatic' }"
              :disabled="working || !automaticAvailable"
              @click="creationMode = 'automatic'"
            >
              Automatic team assignments</button
            ><button
              v-if="!automaticAvailable"
              :class="{ active: creationMode === 'manual' }"
              :disabled="working"
              @click="creationMode = 'manual'"
            >
              Manual draft
            </button>
          </div>
          <p v-if="!automaticAvailable" class="hint mb-[20px]">
            This stage uses manual draft matches.
          </p>
          <div
            v-if="creationMode === 'automatic'"
            class="mb-[20px] rounded-[8px] bg-[#f5f8ee] p-[16px]"
          >
            <p class="text-[12px] mb-[10px]">
              <strong
                >{{ existingMatches.filter((match) => !match.is_bye).length }} game slots</strong
              >
              · {{ awaitingMatches.length }} awaiting teams. These games can receive fields, times
              and referees before the teams are known.
            </p>
            <p class="hint">
              {{
                selectedStage.type === 'individual'
                  ? 'One individual run is reserved for each team.'
                  : selectedStage.type === 'round_robin'
                    ? 'This stage reserves all round-robin fixtures.'
                    : 'This stage contains one round. Use a separate source-seeded stage for the next round.'
              }}
              Slots are prepared automatically when stage settings or teams change.
            </p>
            <RouterLink :to="`/tournaments/${tournamentId}/setup`" class="text-button mt-[10px]"
              >Configure expected teams in stage settings →</RouterLink
            >
          </div>
          <div class="flex flex-wrap items-end justify-between gap-[16px]">
            <label v-if="creationMode === 'manual'" class="max-w-[220px]"
              >Round<input
                v-model.number="round"
                type="number"
                min="1"
                step="1"
                :disabled="working"
                required
            /></label>
            <div class="button-group">
              <template v-if="creationMode === 'automatic'"
                ><button class="button" :disabled="working" @click="prepareSlots">
                  {{ preparing ? 'Preparing slots…' : 'Prepare / update slots' }}</button
                ><button
                  v-if="!assignmentsComplete && !partialAssignments"
                  class="button primary"
                  :disabled="working || !automaticAvailable"
                  @click="previewPairings"
                >
                  {{
                    planning
                      ? 'Preparing preview…'
                      : preview || stalePreview
                        ? 'Preview team assignments again'
                        : 'Preview team assignments'
                  }}
                </button></template
              ><button
                v-else
                class="button"
                :disabled="
                  working ||
                  drafts.length >= 256 ||
                  !stageTeams.length ||
                  !Number.isSafeInteger(round) ||
                  round < 1
                "
                @click="addDraft"
              >
                ＋ Add draft match
              </button>
            </div>
          </div>
          <p v-if="creationMode === 'automatic' && partialAssignments" class="hint mt-[14px]">
            Some slots already have teams. Clear the draw before reviewing a complete automatic
            assignment; fields, times and referees will be retained.
          </p>
          <p v-if="creationMode === 'automatic' && assignmentsComplete" class="hint mt-[14px]">
            All teams are assigned. Clear the draw to repopulate this stage; its scheduled
            reservations will remain.
          </p>
          <p v-if="!stageTeams.length" class="hint mt-[14px]">
            Register teams before populating them. Use the expected-team count in stage settings to
            reserve slots in advance.
          </p>
          <p v-if="creationMode === 'automatic' && !assignmentsComplete" class="hint mt-[14px]">
            Assignments use the registered teams and approved source-stage results. Previewing keeps
            team assignments unchanged; populating fills the saved game slots.
          </p>
          <template v-if="creationMode === 'automatic' && preview">
            <div class="section-title mt-[24px]">
              <h3>Review team assignments</h3>
              <span class="badge warning"
                >{{ preview.matches.length }} team assignments to review</span
              >
            </div>
            <div class="table-container">
              <table>
                <thead>
                  <tr>
                    <th>{{ selectedStage.type === 'individual' ? 'Run' : 'Match' }}</th>
                    <th>Round</th>
                    <th>{{ selectedStage.type === 'individual' ? 'Team / run' : 'Team 1' }}</th>
                    <th v-if="selectedStage.type !== 'individual'">Team 2</th>
                    <th>Duration</th>
                    <th>Reserved field / time</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(match, index) in preview.matches" :key="index">
                    <td>
                      #{{ match.number }}
                      <span v-if="match.is_bye" class="badge">Automatic bye</span>
                    </td>
                    <td>{{ match.round }}</td>
                    <td>
                      {{ teamName(match.team1_id)
                      }}<small v-if="selectedStage.type === 'individual'">Individual run</small>
                    </td>
                    <td v-if="selectedStage.type !== 'individual'">{{ opponentName(match) }}</td>
                    <td>{{ match.duration_minutes }} min</td>
                    <td>
                      {{ fieldName(match)
                      }}<small>{{ formatDate(match.scheduled_at, w.tournament.timezone) }}</small>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-if="!preview.matches.length" class="hint">
              No team assignments are available for these settings.
            </p>
            <p class="hint mt-[14px]">
              Review every assignment before populating teams. Existing fields, times and referee
              assignments stay attached.<span v-if="selectedStage.type !== 'individual'">
                Automatic byes remain unscheduled.</span
              >
            </p>
            <div class="form-actions mt-[20px]">
              <button
                class="button primary"
                :disabled="working || !preview.matches.length"
                @click="submitPairings"
              >
                {{
                  busy
                    ? 'Populating…'
                    : `Populate ${preview.matches.length} ${selectedStage.type === 'individual' ? (preview.matches.length === 1 ? 'run' : 'runs') : 'matches'}`
                }}</button
              ><button class="button" :disabled="working" @click="discardDraft">
                Discard preview
              </button>
            </div>
          </template>
          <form v-if="creationMode === 'manual'" class="mt-[22px]" @submit.prevent="submitManual">
            <p class="hint mb-[16px]">
              Draft matches remain here until you submit the batch. Review each row before
              submitting.
            </p>
            <div v-if="!drafts.length" class="empty compact">Add a draft match to begin.</div>
            <fieldset
              v-for="(draft, index) in drafts"
              :key="draft.key"
              :disabled="working"
              class="mb-[16px] min-w-0 rounded-[9px] border border-[color:var(--border)] p-[18px]"
            >
              <legend class="sr-only">Draft {{ index + 1 }}</legend>
              <div class="section-title">
                <h3>Draft {{ index + 1 }}</h3>
                <button
                  type="button"
                  class="text-button danger-text"
                  @click="drafts = drafts.filter((item) => item.key !== draft.key)"
                >
                  Remove
                </button>
              </div>
              <div class="form-grid">
                <label
                  >Team 1<select v-model="draft.team1_id" required>
                    <option :value="0" disabled>Select team</option>
                    <option v-for="team in stageTeams" :key="team.id" :value="team.id">
                      {{ team.name }}
                    </option>
                  </select></label
                ><label
                  >Team 2<select
                    v-model="draft.team2_id"
                    :required="selectedLeague?.mode !== 'scheduling'"
                  >
                    <option :value="null" :disabled="selectedLeague?.mode !== 'scheduling'">
                      {{
                        selectedLeague?.mode === 'scheduling'
                          ? 'Individual run (no opponent)'
                          : 'Select opponent'
                      }}
                    </option>
                    <option v-for="team in stageTeams" :key="team.id" :value="team.id">
                      {{ team.name }}
                    </option>
                  </select></label
                ><label
                  >Duration (minutes)<input
                    v-model.number="draft.duration_minutes"
                    type="number"
                    min="1"
                    max="1440"
                    step="1"
                    required /></label
                ><label
                  >Round<input
                    v-model.number="draft.round"
                    type="number"
                    min="1"
                    step="1"
                    required /></label
                ><label class="full-width"
                  >Notes<textarea v-model="draft.notes" rows="2" maxlength="4000"></textarea>
                </label>
              </div>
            </fieldset>
            <div class="form-actions">
              <button type="submit" class="button primary" :disabled="working || !drafts.length">
                {{ busy ? 'Submitting…' : `Submit ${drafts.length} matches` }}</button
              ><button
                type="button"
                class="button"
                :disabled="working || !drafts.length"
                @click="discardDraft"
              >
                Discard draft
              </button>
            </div>
          </form>
        </section>
        <section class="panel">
          <div class="section-title">
            <div>
              <h2>Existing stage matches</h2>
              <p>{{ existingMatches.length }} matches in {{ selectedStage.name }}</p>
            </div>
            <div class="button-group">
              <button
                v-if="canAdmin"
                class="button small danger"
                :disabled="working || !existingMatches.some((match) => !match.is_placeholder)"
                @click="openClearDialog"
              >
                Clear draw</button
              ><button class="button small" :disabled="working || loading" @click="reload">
                ↻ Refresh
              </button>
            </div>
          </div>
          <div v-if="!existingMatches.length" class="empty compact">
            {{
              automaticAvailable
                ? 'Set the expected-team count in stage settings, then prepare game slots for scheduling.'
                : 'Submitted matches will appear here. Prepare and review a draft above to create them.'
            }}
          </div>
          <div v-else class="table-container">
            <table>
              <thead>
                <tr>
                  <th>Match</th>
                  <th>Round</th>
                  <th>Teams</th>
                  <th>Duration</th>
                  <th>Field / time</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="match in existingMatches" :key="match.id">
                  <td>#{{ match.number }}<span v-if="match.is_bye" class="badge">Bye</span></td>
                  <td>{{ match.round }}</td>
                  <td>
                    <strong>{{
                      match.is_placeholder
                        ? selectedStage.type === 'individual'
                          ? 'Awaiting team'
                          : 'Awaiting teams'
                        : teamName(match.team1_id)
                    }}</strong
                    ><small>{{
                      match.is_placeholder
                        ? 'Populate teams from the stage settings when ready'
                        : opponentName(match)
                    }}</small>
                  </td>
                  <td>{{ match.duration_minutes }} min</td>
                  <td>
                    {{ fieldName(match)
                    }}<small>{{ formatDate(match.scheduled_at, w.tournament.timezone) }}</small>
                  </td>
                  <td>
                    <span class="badge" :class="match.status === 'approved' ? 'success' : ''">{{
                      match.is_placeholder
                        ? selectedStage.type === 'individual'
                          ? 'Awaiting team'
                          : 'Awaiting teams'
                        : match.status.replace('_', ' ')
                    }}</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-if="!canAdmin" class="hint mt-[18px]">
            An administrator can prepare and submit new stage matches.
          </p>
        </section>
      </template>
      <section v-else-if="!selectionOpen" class="panel empty">
        <h2>No stages in this league</h2>
        <p>Choose another league or add a stage in tournament setup.</p>
      </section>
    </template>
  </template>
  <dialog
    ref="clearDialog"
    class="clear-draw-dialog panel fixed inset-0 m-auto max-h-[calc(100vh-48px)] w-[min(550px,calc(100vw-32px))] overflow-y-auto"
    aria-labelledby="clear-draw-title"
    @cancel="cancelClearDialog"
  >
    <template v-if="clearTarget"
      ><div class="section-title">
        <h2 id="clear-draw-title">
          {{ clearResetsAssignments ? 'Clear team assignments' : 'Clear draw' }} ·
          {{ clearTarget.name }}
        </h2>
        <button
          type="button"
          class="icon-button"
          aria-label="Close clear draw confirmation"
          :disabled="clearing"
          @click="closeClearDialog"
        >
          ×
        </button>
      </div>
      <p class="text-[12px] text-muted">
        {{
          clearResetsAssignments
            ? 'Choose which team assignments to reset to empty match slots.'
            : 'Choose which saved matches to clear from this stage.'
        }}
      </p>
      <fieldset class="my-[20px] min-w-0 space-y-[14px] border-0 p-0" :disabled="clearing">
        <legend class="sr-only">Matches to clear</legend>
        <label class="checkbox"
          ><input v-model="clearScope" type="radio" value="all" /><span
            >{{ clearResetsAssignments ? 'All team assignments' : 'All stage matches' }}
            <strong>({{ clearAffectedMatches.length }})</strong></span
          ></label
        ><label v-if="allowLatestClear" class="checkbox"
          ><input v-model="clearScope" type="radio" value="latest" /><span
            >Latest round ({{ latestClearRound }})
            <strong
              >({{
                clearAffectedMatches.filter((match) => match.round === latestClearRound).length
              }}
              matches)</strong
            ></span
          ></label
        >
      </fieldset>
      <p v-if="clearResetsAssignments" class="text-[12px]">
        This clears team assignments from
        <strong>{{ clearCount }} {{ clearTarget.type === 'individual' ? 'runs' : 'games' }}</strong
        ><span v-if="clearTarget.type !== 'individual'"> and removes generated byes</span>. Empty
        game slots retain their fields, times, assigned referees and publication settings. Stage
        settings and registered teams remain.
      </p>
      <p v-else class="text-[12px]">
        This removes <strong>{{ clearCount }} saved matches</strong>, including their field and time
        assignments, referee assignments, match notes and published schedule rows. The stage, its
        settings and registered teams are retained.
      </p>
      <p class="hint mb-[20px]">
        Only unstarted matches and automatic byes can be cleared. Started matches, recorded results
        or later draws that depend on these games block clearing.
      </p>
      <p v-if="clearError" class="error" role="alert">{{ clearError }}</p>
      <div class="form-actions">
        <button
          type="button"
          class="button danger"
          :disabled="clearing || !clearCount"
          @click="clearDraw"
        >
          {{
            clearing
              ? 'Clearing…'
              : clearResetsAssignments
                ? `Clear ${clearCount} team assignments`
                : `Clear ${clearCount} matches`
          }}</button
        ><button
          type="button"
          class="button"
          :disabled="clearing"
          autofocus
          @click="closeClearDialog"
        >
          Cancel
        </button>
      </div></template
    >
  </dialog>
</template>

<style scoped>
.clear-draw-dialog::backdrop {
  background: #0b182b85;
  backdrop-filter: blur(3px);
}
</style>
