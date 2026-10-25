<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useWorkspace } from '@/composables/workspace'
import { api, canAdmin, canEdit, formatDate } from '@/lib/api'
import type { Match, Referee, Slot } from '@/lib/types'
const {
  workspace: w,
  loading,
  error,
  busy,
  tournamentId,
  perform,
  reload,
  teamName,
  leagueName,
  stageName,
} = useWorkspace()
const leagueFilter = ref(0),
  stageFilter = ref(0),
  selectedDay = ref('')
const restoredTournamentId = ref<number | null>(null)
const workspaceReady = computed(() => w.value?.tournament.id === tournamentId.value)
const mainLeagues = computed(() => w.value?.leagues.filter((league) => !league.parent_id) ?? [])
const availableStages = computed(
  () => w.value?.stages.filter((stage) => leagueMatches(stage.league_id)) ?? [],
)
const selected = ref<Match | null>(null),
  selectedSlot = ref<number | null>(null),
  selectedRefs = ref<number[]>([]),
  dragId = ref<number | null>(null),
  dropTarget = ref<number | null>(null)
const canEditMetadata = computed(
  () => selected.value && ['draft', 'scheduled'].includes(selected.value.status),
)
const selectedStage = computed(() =>
  w.value?.stages.find((stage) => stage.id === selected.value?.stage_id),
)
const originalStage = computed(() => {
  const original = w.value?.matches.find((match) => match.id === selected.value?.id)
  return w.value?.stages.find((stage) => stage.id === original?.stage_id)
})
const stageMetadataLocked = computed(
  () => !!selected.value?.is_placeholder || originalStage.value?.type !== 'manual',
)
const metadataStages = computed(() =>
  stageMetadataLocked.value
    ? originalStage.value
      ? [originalStage.value]
      : []
    : (w.value?.stages.filter(
        (stage) => stage.league_id === selected.value?.league_id && stage.type === 'manual',
      ) ?? []),
)
const selectedIsIndividual = computed(() => selectedStage.value?.type === 'individual')
const selectedAllowsNoOpponent = computed(
  () =>
    selectedStage.value?.type === 'manual' &&
    w.value?.leagues.find((league) => league.id === selected.value?.league_id)?.mode ===
      'scheduling',
)
watch(
  () => selected.value?.stage_id,
  () => {
    if (selected.value && selectedIsIndividual.value) selected.value.team2_id = null
  },
  { flush: 'sync' },
)
function endDrag() {
  dragId.value = null
  dropTarget.value = null
}
function chooseEmptySlot(id: number) {
  selectedSlot.value = id
  selected.value = null
}
function dateKey(value: string) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: w.value?.tournament.timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date(value))
  return `${parts.find((p) => p.type === 'year')?.value}-${parts.find((p) => p.type === 'month')?.value}-${parts.find((p) => p.type === 'day')?.value}`
}
function leagueMatches(id: number) {
  if (!leagueFilter.value) return true
  const seen = new Set<number>()
  let current = w.value?.leagues.find((league) => league.id === id)
  while (current && !seen.has(current.id)) {
    if (current.id === leagueFilter.value) return true
    seen.add(current.id)
    current = w.value?.leagues.find((league) => league.id === current?.parent_id)
  }
  return false
}
function storageKey(id: number) {
  return `schedule.mainLeague.${id}`
}
function restoreMainLeague(id: number) {
  try {
    const saved = localStorage.getItem(storageKey(id))
    if (saved === null) return 0
    const value = /^(0|[1-9]\d*)$/.test(saved) ? Number(saved) : NaN
    if (
      Number.isSafeInteger(value) &&
      (value === 0 || mainLeagues.value.some((league) => league.id === value))
    )
      return value
    localStorage.removeItem(storageKey(id))
  } catch {
    /* Filtering remains available when browser storage is disabled. */
  }
  return 0
}
function persistMainLeague(id: number, value: number) {
  try {
    localStorage.setItem(storageKey(id), String(value))
  } catch {
    /* Keep the in-memory selection. */
  }
}
const filteredMatches = computed(
  () =>
    w.value?.matches.filter(
      (m) => leagueMatches(m.league_id) && (!stageFilter.value || m.stage_id === stageFilter.value),
    ) ?? [],
)
const fields = computed(
  () => w.value?.fields.filter((field) => leagueMatches(field.league_id)) ?? [],
)
watch(
  [tournamentId, w],
  ([id, workspace]) => {
    if (!workspace || workspace.tournament.id !== id) {
      restoredTournamentId.value = null
      return
    }
    if (restoredTournamentId.value !== id) {
      restoredTournamentId.value = id
      leagueFilter.value = restoreMainLeague(id)
      stageFilter.value = 0
      selectedDay.value = ''
      selected.value = null
      selectedSlot.value = null
      endDrag()
    } else if (
      leagueFilter.value &&
      !mainLeagues.value.some((league) => league.id === leagueFilter.value)
    ) {
      leagueFilter.value = 0
      persistMainLeague(id, 0)
    }
    if (stageFilter.value && !availableStages.value.some((stage) => stage.id === stageFilter.value))
      stageFilter.value = 0
  },
  { immediate: true },
)
watch(leagueFilter, (value) => {
  stageFilter.value = 0
  selectedDay.value = ''
  selectedSlot.value = null
  selected.value = null
  endDrag()
  if (workspaceReady.value && restoredTournamentId.value === tournamentId.value)
    persistMainLeague(tournamentId.value, value)
})
const allSlots = computed(
  () => w.value?.slots.filter((s) => fields.value.some((f) => f.id === s.field_id)) ?? [],
)
const days = computed(() => [...new Set(allSlots.value.map((s) => dateKey(s.starts_at)))].sort())
const day = computed(() =>
  days.value.includes(selectedDay.value) ? selectedDay.value : (days.value[0] ?? ''),
)
const daySlots = computed(() => allSlots.value.filter((s) => dateKey(s.starts_at) === day.value))
const times = computed(() => [...new Set(daySlots.value.map((s) => s.starts_at))].sort())
const unassigned = computed(() => filteredMatches.value.filter((m) => !m.slot_id && !m.is_bye))
const scheduledCount = computed(() => filteredMatches.value.filter((m) => m.slot_id).length)
const publishedCount = computed(() => filteredMatches.value.filter((m) => m.published).length)
const targetSlot = computed(() => w.value?.slots.find((s) => s.id === selectedSlot.value))
const eligibleSlots = computed(
  () =>
    w.value?.slots.filter((s) => {
      const f = w.value?.fields.find((f) => f.id === s.field_id)
      const l = w.value?.leagues.find((l) => l.id === selected.value?.league_id)
      return f && (f.league_id === l?.id || f.league_id === l?.parent_id)
    }) ?? [],
)
function opponentName(match: Match) {
  if (match.is_placeholder) return 'TBD'
  return match.team2_id ? teamName(match.team2_id) : match.is_bye ? 'Bye' : 'Individual run'
}
function isIndividualMatch(match: Match) {
  return w.value?.stages.find((stage) => stage.id === match.stage_id)?.type === 'individual'
}
function firstTeamName(match: Match) {
  return match.is_placeholder
    ? isIndividualMatch(match)
      ? 'Awaiting team'
      : 'Awaiting teams'
    : teamName(match.team1_id)
}
function slotAt(fieldId: number, time: string) {
  return daySlots.value.find((s) => s.field_id === fieldId && s.starts_at === time)
}
function matchAt(slotId: number) {
  return w.value?.matches.find((m) => m.slot_id === slotId)
}
function matchVisibleAt(slotId: number) {
  const match = matchAt(slotId)
  return (
    !!match &&
    leagueMatches(match.league_id) &&
    (!stageFilter.value || match.stage_id === stageFilter.value)
  )
}
function openMatch(match: Match, slotId?: number) {
  if (!canEdit.value) return
  selected.value = { ...match }
  selectedSlot.value = slotId ?? match.slot_id
  selectedRefs.value = [...(match.referee_ids ?? [])]
}
function dragStart(event: DragEvent, match: Match) {
  if (!canEdit.value) return
  dragId.value = match.id
  event.dataTransfer?.setData('text/plain', String(match.id))
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}
function drop(event: DragEvent, slot: Slot) {
  event.preventDefault()
  dropTarget.value = null
  const id = Number(event.dataTransfer?.getData('text/plain')) || dragId.value
  const match = w.value?.matches.find((m) => m.id === id)
  if (match && !matchAt(slot.id)) openMatch(match, slot.id)
  dragId.value = null
}
async function saveAssignment() {
  if (!selected.value) return
  if (selectedIsIndividual.value) selected.value.team2_id = null
  if (
    await perform(async () => {
      if (canEditMetadata.value) await api.put(`/matches/${selected.value!.id}`, selected.value)
      await api.post(`/matches/${selected.value!.id}/schedule`, {
        slot_id: selectedSlot.value,
        referee_ids: selectedRefs.value,
      })
    })
  )
    selected.value = null
}
async function publish(match: Match) {
  await perform(() => api.post(`/matches/${match.id}/publish`, { published: !match.published }))
}
async function deleteMatch() {
  if (
    selected.value &&
    confirm('Delete this match?') &&
    (await perform(() => api.delete(`/matches/${selected.value!.id}`)))
  )
    selected.value = null
}
function refereeAvailability(referee: Referee) {
  const slot = targetSlot.value
  if (!slot || !selected.value) return ''
  const start = new Date(slot.starts_at).getTime(),
    end = start + selected.value.duration_minutes * 60000
  if (referee.available_from && start < new Date(referee.available_from).getTime())
    return 'Not yet available'
  if (referee.available_until && end > new Date(referee.available_until).getTime())
    return 'Availability ends before match'
  const conflict = w.value?.matches.find(
    (m) =>
      m.id !== selected.value!.id &&
      (m.referee_ids ?? []).includes(referee.id) &&
      m.scheduled_at &&
      new Date(m.scheduled_at).getTime() < end &&
      new Date(m.scheduled_at).getTime() + m.duration_minutes * 60000 > start,
  )
  return conflict ? `Assigned to match #${conflict.number}` : ''
}
function dayLabel(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    timeZone: 'UTC',
  }).format(new Date(`${value}T12:00:00Z`))
}
</script>
<template>
  <div
    class="page-heading flex items-center justify-between gap-[20px] max-[1200px]:items-start max-[760px]:gap-[18px]"
  >
    <div>
      <p class="eyebrow">COMPETITION PLANNING</p>
      <h1>Match schedule</h1>
      <p>Build the day. Keep every field, team and referee in sync.</p>
    </div>
    <div class="button-group flex flex-wrap items-center gap-[9px]">
      <button class="button" :disabled="loading" @click="reload">↻ Refresh</button>
      <RouterLink
        :to="{
          name: 'stage-games',
          params: { id: tournamentId },
          query: stageFilter ? { stage: stageFilter } : {},
        }"
        class="button primary"
        >Stage games →</RouterLink
      >
    </div>
  </div>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <div v-if="loading && !workspaceReady" class="loading">Loading match schedule…</div>
  <template v-if="w && workspaceReady">
    <div class="schedule-summary">
      <span
        ><strong>{{ filteredMatches.length }}</strong> total matches</span
      ><span
        ><i class="legend-dot green"></i><strong>{{ scheduledCount }}</strong> scheduled</span
      ><span
        ><i class="legend-dot amber"></i><strong>{{ unassigned.length }}</strong> unassigned</span
      ><span
        ><i class="legend-dot blue"></i><strong>{{ publishedCount }}</strong> published</span
      ><span class="timezone-label">◷ {{ w.tournament.timezone }}</span>
    </div>
    <div class="schedule-toolbar">
      <div class="tabs day-tabs">
        <button v-for="d in days" :key="d" :class="{ active: day === d }" @click="selectedDay = d">
          {{ dayLabel(d) }}</button
        ><span v-if="!days.length" class="muted">No available days</span>
      </div>
      <div class="filter-group">
        <label
          ><span class="sr-only">Main league</span
          ><select v-model="leagueFilter" aria-label="Main league">
            <option :value="0">All main leagues</option>
            <option v-for="l in mainLeagues" :key="l.id" :value="l.id">{{ l.name }}</option>
          </select></label
        ><label
          ><span class="sr-only">Stage filter</span
          ><select v-model="stageFilter">
            <option :value="0">All stages</option>
            <option v-for="s in availableStages" :key="s.id" :value="s.id">
              {{ leagueName(s.league_id) }} · {{ s.name }}
            </option>
          </select></label
        >
      </div>
    </div>
    <div class="schedule-layout">
      <section class="schedule-board panel">
        <div v-if="!w.fields.length || !w.slots.length" class="empty">
          <span class="empty-icon">▦</span>
          <h2>Your schedule is ready to take shape</h2>
          <p>
            Add fields and available timeslots in tournament setup, then place your matches here.
          </p>
          <RouterLink
            v-if="canAdmin"
            :to="`/tournaments/${tournamentId}/setup`"
            class="button primary"
            >Configure fields & timeslots</RouterLink
          >
        </div>
        <div v-else-if="!daySlots.length" class="empty">No timeslots match this filter.</div>
        <div v-else class="schedule-scroll">
          <div
            class="schedule-grid"
            :style="{ gridTemplateColumns: `72px repeat(${fields.length}, minmax(210px, 1fr))` }"
          >
            <div class="time-heading">TIME</div>
            <div v-for="f in fields" :key="f.id" class="field-heading">
              <span class="field-icon">▣</span>
              <div>
                <strong>{{ f.name }}</strong
                ><small>{{ leagueName(f.league_id) }}</small>
              </div>
            </div>
            <template v-for="time in times" :key="time"
              ><div class="time-cell">
                {{ formatDate(time, w.tournament.timezone, { month: undefined, day: undefined }) }}
              </div>
              <div
                v-for="f in fields"
                :key="`${f.id}-${time}`"
                class="schedule-cell"
                :class="{
                  unavailable: !slotAt(f.id, time),
                  'drop-active': dropTarget === slotAt(f.id, time)?.id,
                }"
                @dragover.prevent="dropTarget = slotAt(f.id, time)?.id ?? null"
                @dragleave="dropTarget = null"
                @drop="slotAt(f.id, time) && drop($event, slotAt(f.id, time)!)"
              >
                <template v-if="slotAt(f.id, time)"
                  ><article
                    v-if="matchVisibleAt(slotAt(f.id, time)!.id)"
                    class="match-card"
                    :class="{ published: matchAt(slotAt(f.id, time)!.id)!.published }"
                    :draggable="canEdit"
                    @dragstart="dragStart($event, matchAt(slotAt(f.id, time)!.id)!)"
                    @dragend="endDrag"
                  >
                    <button
                      class="match-card-main"
                      :disabled="!canEdit"
                      @click="openMatch(matchAt(slotAt(f.id, time)!.id)!)"
                    >
                      <span class="match-meta"
                        >#{{ matchAt(slotAt(f.id, time)!.id)!.number }}
                        <span>{{
                          stageName(matchAt(slotAt(f.id, time)!.id)!.stage_id)
                        }}</span></span
                      ><strong>{{ firstTeamName(matchAt(slotAt(f.id, time)!.id)!) }}</strong
                      ><span
                        v-if="isIndividualMatch(matchAt(slotAt(f.id, time)!.id)!)"
                        class="versus"
                        >Individual run</span
                      >
                      <template v-else
                        ><span class="versus">vs</span
                        ><strong>{{
                          opponentName(matchAt(slotAt(f.id, time)!.id)!)
                        }}</strong></template
                      >
                      <small>{{
                        (matchAt(slotAt(f.id, time)!.id)!.referee_ids ?? [])
                          .map((id) => w!.referees.find((r) => r.id === id)?.name)
                          .filter(Boolean)
                          .join(' · ') || 'No referees assigned'
                      }}</small>
                    </button>
                    <div class="match-card-footer">
                      <span
                        class="badge"
                        :class="matchAt(slotAt(f.id, time)!.id)!.published ? 'success' : ''"
                        >{{
                          matchAt(slotAt(f.id, time)!.id)!.published ? 'Public' : 'Unpublished'
                        }}</span
                      ><button
                        v-if="canEdit"
                        class="text-button"
                        :disabled="busy"
                        @click="publish(matchAt(slotAt(f.id, time)!.id)!)"
                      >
                        {{ matchAt(slotAt(f.id, time)!.id)!.published ? 'Unpublish' : 'Publish' }}
                      </button>
                    </div>
                  </article>
                  <div v-else-if="matchAt(slotAt(f.id, time)!.id)" class="empty-slot muted">
                    Occupied by another stage
                  </div>
                  <button
                    v-else-if="canEdit"
                    class="empty-slot"
                    @click="chooseEmptySlot(slotAt(f.id, time)!.id)"
                  >
                    <span>＋</span>Drop a match here<small
                      >{{
                        formatDate(slotAt(f.id, time)!.ends_at, w.tournament.timezone, {
                          month: undefined,
                          day: undefined,
                        })
                      }}
                      end</small
                    >
                  </button>
                  <div v-else class="empty-slot">Available</div></template
                >
              </div></template
            >
          </div>
        </div>
      </section>
      <aside class="unassigned-panel panel">
        <div class="section-title flex items-center justify-between gap-[12px]">
          <h2>Unassigned</h2>
          <span class="badge">{{ unassigned.length }}</span>
        </div>
        <p class="hint">
          {{
            canEdit
              ? 'Drag a match onto a timeslot, or choose a match to assign it.'
              : 'Matches awaiting a timeslot.'
          }}
        </p>
        <div v-if="!unassigned.length" class="empty compact">
          <span>✓</span>
          <p>Every match has a place.</p>
        </div>
        <article
          v-for="m in unassigned"
          :key="m.id"
          class="unassigned-match"
          :draggable="canEdit"
          @dragstart="dragStart($event, m)"
          @dragend="dragId = null"
        >
          <button :disabled="!canEdit" @click="openMatch(m, selectedSlot ?? undefined)">
            <span class="match-meta"
              >#{{ m.number }}
              <span>{{ m.duration_minutes }} min <span v-if="canEdit">⠿</span></span></span
            ><strong>{{ firstTeamName(m) }}</strong
            ><span class="versus">{{
              isIndividualMatch(m) ? 'Individual run' : `vs ${opponentName(m)}`
            }}</span
            ><small>{{ leagueName(m.league_id) }} · {{ stageName(m.stage_id) }}</small>
          </button>
        </article>
        <p v-if="selectedSlot && !selected" class="hint">
          Choose an unassigned match to place it in the selected timeslot.
        </p>
      </aside>
    </div>
  </template>
  <div v-if="selected && w && workspaceReady" class="modal-backdrop" @click.self="selected = null">
    <section class="modal panel" role="dialog" aria-modal="true" aria-labelledby="assign-title">
      <div class="section-title flex items-center justify-between gap-[12px]">
        <h2 id="assign-title">Assign match #{{ selected.number }}</h2>
        <button class="icon-button" aria-label="Close" @click="selected = null">×</button>
      </div>
      <p class="match-title">
        {{ firstTeamName(selected) }}
        <span class="muted">{{ isIndividualMatch(selected) ? '· Individual run' : 'vs' }}</span>
        <template v-if="!isIndividualMatch(selected)">{{ opponentName(selected) }}</template>
      </p>
      <form class="stack" @submit.prevent="saveAssignment">
        <details v-if="canEditMetadata" class="metadata-editor">
          <summary>Edit match details</summary>
          <div class="form-grid">
            <label
              >Stage<select v-model="selected.stage_id" :disabled="stageMetadataLocked">
                <option v-for="s in metadataStages" :key="s.id" :value="s.id">
                  {{ s.name }}
                </option>
              </select></label
            ><label
              >Duration (minutes)<input
                v-model.number="selected.duration_minutes"
                type="number"
                min="1"
                max="1440"
                required /></label
            ><label v-if="!selected.is_placeholder"
              >Team 1<select v-model="selected.team1_id" required>
                <option
                  v-for="t in w.teams.filter(
                    (t) => t.league_id === selected!.league_id && t.id !== selected!.team2_id,
                  )"
                  :key="t.id"
                  :value="t.id"
                >
                  {{ t.name }}
                </option>
              </select></label
            ><label v-if="!selected.is_placeholder && !selectedIsIndividual"
              >Team 2<select v-model="selected.team2_id">
                <option :value="null" :disabled="!selectedAllowsNoOpponent">
                  {{ selectedAllowsNoOpponent ? 'Individual run' : 'Select opponent' }}
                </option>
                <option
                  v-for="t in w.teams.filter(
                    (t) => t.league_id === selected!.league_id && t.id !== selected!.team1_id,
                  )"
                  :key="t.id"
                  :value="t.id"
                >
                  {{ t.name }}
                </option>
              </select></label
            ><label
              >Round<input
                v-model.number="selected.round"
                type="number"
                min="1"
                required
                :disabled="selected.is_placeholder" /></label
            ><label class="full-width"
              >Notes<textarea v-model="selected.notes" rows="2"></textarea>
            </label>
          </div>
          <p v-if="selected.is_placeholder" class="hint">
            {{ selectedIsIndividual ? 'The team will be filled' : 'Teams will be filled' }} from
            Stage games. This booking stays reserved for the same stage and round.
          </p>
          <p v-else-if="selectedIsIndividual" class="hint">
            Individual runs have one team and no opponent.
          </p>
        </details>
        <label
          >Timeslot<select v-model="selectedSlot">
            <option :value="null">Unscheduled</option>
            <option
              v-for="s in eligibleSlots"
              :key="s.id"
              :value="s.id"
              :disabled="Boolean(matchAt(s.id) && matchAt(s.id)?.id !== selected.id)"
            >
              {{ w.fields.find((f) => f.id === s.field_id)?.name }} ·
              {{ formatDate(s.starts_at, w.tournament.timezone)
              }}{{ matchAt(s.id) && matchAt(s.id)?.id !== selected.id ? ' · Occupied' : '' }}
            </option>
          </select></label
        >
        <fieldset>
          <legend>Referees</legend>
          <p v-if="!w.referees.length" class="hint">
            No referees registered. Add them in tournament setup.
          </p>
          <label v-for="r in w.referees" :key="r.id" class="checkbox referee-option"
            ><input
              v-model="selectedRefs"
              :value="r.id"
              type="checkbox"
              :disabled="Boolean(refereeAvailability(r)) && !selectedRefs.includes(r.id)"
            /><span
              >{{ r.name
              }}<small :class="refereeAvailability(r) ? 'danger-text' : 'success-text'">{{
                refereeAvailability(r) || 'Available'
              }}</small></span
            ></label
          >
        </fieldset>
        <p class="hint">Field, team and referee conflicts are checked when saving.</p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
          <button class="button primary" :disabled="busy">Save assignment</button
          ><button type="button" class="button" @click="selected = null">Cancel</button
          ><button
            v-if="canAdmin && !selected.is_placeholder"
            type="button"
            class="button danger"
            :disabled="busy"
            @click="deleteMatch"
          >
            Delete
          </button>
        </div>
      </form>
    </section>
  </div>
</template>
