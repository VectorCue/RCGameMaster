<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { errorMessage, publicApi } from '@/lib/api'
import type { Match, MatchEvent, Workspace } from '@/lib/types'

const route = useRoute()
const workspace = ref<Workspace | null>(null)
const loading = ref(true)
const error = ref('')
const fieldReference = computed(() =>
  Object.prototype.hasOwnProperty.call(route.params, 'fieldId')
    ? route.params.fieldId
    : Object.prototype.hasOwnProperty.call(route.query, 'field')
      ? route.query.field
      : undefined,
)
const fieldRequested = computed(() => fieldReference.value !== undefined)
const fieldId = computed(() => {
  const value = fieldReference.value
  if (typeof value !== 'string' || !/^\d+$/.test(value)) return null
  const id = Number(value)
  return Number.isSafeInteger(id) && id > 0 ? id : null
})
const selectedField = computed(() =>
  workspace.value?.fields.find((field) => field.id === fieldId.value),
)
const fieldUnavailable = computed(
  () => !!workspace.value && fieldRequested.value && !selectedField.value,
)
const now = ref(Date.now())
const updatedAt = ref(0)
let tick: ReturnType<typeof setInterval> | undefined
let poll: ReturnType<typeof setInterval> | undefined
let serverOffset = 0
let loadSequence = 0
const timezone = computed(() => workspace.value?.tournament.timezone || 'UTC')
const fieldMatches = computed(() =>
  fieldUnavailable.value
    ? []
    : (workspace.value?.matches ?? []).filter(
        (m) => m.published && (!fieldRequested.value || m.field_id === fieldId.value),
      ),
)
const eventRows = computed(() =>
  fieldMatches.value
    .flatMap((match) => {
      const goalsVisible =
        match.status === 'approved' ||
        (match.status === 'in_progress' &&
          ['first_half', 'halftime', 'second_half'].includes(match.live_state?.phase || ''))
      return (match.live_state?.events || [])
        .filter(
          (event) =>
            Number.isFinite(Date.parse(event.at)) &&
            (goalsVisible || !['goal', 'goal_removed'].includes(event.type)),
        )
        .map((event) => ({ match, event }))
    })
    .sort(
      (a, b) =>
        Date.parse(b.event.at) - Date.parse(a.event.at) ||
        b.event.id - a.event.id ||
        b.match.id - a.match.id,
    )
    .slice(0, 100),
)
const upcoming = computed(() =>
  fieldMatches.value
    .filter((m) => !['approved', 'submitted'].includes(m.status))
    .sort(
      (a, b) =>
        (a.status === 'in_progress' ? 0 : 1) - (b.status === 'in_progress' ? 0 : 1) ||
        (a.scheduled_at || '9999').localeCompare(b.scheduled_at || '9999') ||
        a.number - b.number,
    ),
)
const featured = computed(() => upcoming.value[0])
const hasLiveScore = computed(
  () =>
    featured.value?.status === 'in_progress' &&
    ['first_half', 'halftime', 'second_half'].includes(featured.value.live_state?.phase || ''),
)
const results = computed(() =>
  fieldMatches.value
    .filter((m) => m.status === 'approved')
    .sort((a, b) => (b.scheduled_at || '').localeCompare(a.scheduled_at || ''))
    .slice(0, 4),
)
const timeOfDay = computed(() =>
  new Intl.DateTimeFormat([], {
    timeZone: timezone.value,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(now.value),
)
const day = computed(() =>
  new Intl.DateTimeFormat([], {
    timeZone: timezone.value,
    weekday: 'long',
    month: 'long',
    day: 'numeric',
  }).format(now.value),
)
const timer = computed(() => {
  const match = featured.value
  if (!match) return null
  const live = match.live_state
  if (match.status === 'in_progress' && live) {
    const elapsed =
      live.running && live.started_at ? (now.value - new Date(live.started_at).getTime()) / 1000 : 0
    return Math.max(0, Math.ceil(live.remaining_seconds - elapsed))
  }
  return match.scheduled_at
    ? Math.max(0, Math.ceil((new Date(match.scheduled_at).getTime() - now.value) / 1000))
    : null
})
const phase = computed(() => {
  const match = featured.value
  if (!match) return 'Next on the field'
  if (match.status !== 'in_progress') return timer.value === 0 ? 'Starting soon' : 'Kickoff in'
  const live = match.live_state
  const label = (
    {
      pregame: 'Inspection',
      ready: 'Ready for kickoff',
      first_half: 'First half',
      halftime: 'Half-time',
      second_half: 'Second half',
      finished: 'Awaiting review',
    } as Record<string, string>
  )[live?.phase || 'ready']
  return `${label}${live?.running || live?.phase === 'finished' ? '' : ' · Paused'}`
})
const teamName = (id: number | null | undefined) =>
  workspace.value?.teams.find((t) => t.id === id)?.name || (id === 0 ? 'Awaiting teams' : 'Bye')
function opponentName(match: Match) {
  if (match.is_placeholder) return 'Awaiting teams'
  return match.team2_id ? teamName(match.team2_id) : match.is_bye ? 'Bye' : 'Individual run'
}
function individualRun(match: Match) {
  return (
    workspace.value?.stages.find((stage) => stage.id === match.stage_id)?.type === 'individual' ||
    (!match.is_placeholder && !match.is_bye && !match.team2_id)
  )
}
function firstTeamName(match: Match) {
  return individualRun(match) && match.is_placeholder ? 'Awaiting team' : teamName(match.team1_id)
}
const fieldName = (id: number | null | undefined) =>
  workspace.value?.fields.find((f) => f.id === id)?.name || 'Field to be assigned'
const leagueName = (id: number | undefined) =>
  workspace.value?.leagues.find((l) => l.id === id)?.name || ''
const stageName = (id: number | undefined) =>
  workspace.value?.stages.find((s) => s.id === id)?.name || ''
function scheduledTime(match: Match) {
  return match.scheduled_at
    ? new Intl.DateTimeFormat([], {
        timeZone: timezone.value,
        hour: '2-digit',
        minute: '2-digit',
      }).format(new Date(match.scheduled_at))
    : 'TBA'
}
function clock(seconds: number) {
  const hours = Math.floor(seconds / 3600)
  const min = Math.floor((seconds % 3600) / 60)
  const sec = seconds % 60
  return `${hours ? `${String(hours).padStart(2, '0')}:` : ''}${String(min).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}
function eventTime(event: MatchEvent) {
  return new Intl.DateTimeFormat([], {
    timeZone: timezone.value,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(event.at))
}
function eventPhase(phase: string) {
  return (
    (
      {
        pregame: 'Pregame',
        ready: 'Kickoff',
        first_half: 'First half',
        halftime: 'Half-time',
        second_half: 'Second half',
        finished: 'Full time',
      } as Record<string, string>
    )[phase] || phase
  )
}
function eventLabel(event: MatchEvent) {
  switch (event.type) {
    case 'match_started':
      return 'Match started'
    case 'clock_started':
      return 'Clock started'
    case 'clock_paused':
      return 'Clock paused'
    case 'goal':
      return 'Goal scored'
    case 'goal_removed':
      return 'Goal removed'
    case 'robot_timeout':
      return `Robot ${event.robot} · timeout started`
    case 'robot_timeout_reset':
      return `Robot ${event.robot} · timeout reset to 1:00`
    case 'robot_returned':
      return `Robot ${event.robot} · returned to play`
    case 'period_advanced':
      return `${event.forced ? 'Skipped' : 'Advanced'} to ${eventPhase(event.next_phase || '')}`
    case 'match_ended':
      return event.forced ? 'Match finished · period skipped' : 'Match finished'
    case 'match_ended_early':
      return 'Match ended early'
    default:
      return event.type.replaceAll('_', ' ')
  }
}
async function load() {
  const abbrev = String(route.params.abbrev)
  const sequence = ++loadSequence
  try {
    const response = await publicApi.get<Workspace>(`/${encodeURIComponent(abbrev)}`)
    if (sequence !== loadSequence || abbrev !== String(route.params.abbrev)) return
    const serverTime = Date.parse(String(response.headers['x-server-time'] || ''))
    if (Number.isFinite(serverTime)) serverOffset = serverTime - Date.now()
    now.value = Date.now() + serverOffset
    workspace.value = response.data
    updatedAt.value = now.value
    error.value = ''
  } catch (err) {
    if (sequence === loadSequence && abbrev === String(route.params.abbrev))
      error.value = errorMessage(err)
  } finally {
    if (sequence === loadSequence && abbrev === String(route.params.abbrev)) loading.value = false
  }
}
watch(
  () => route.params.abbrev,
  async () => {
    workspace.value = null
    loading.value = true
    await load()
  },
)
onMounted(async () => {
  await load()
  tick = setInterval(() => {
    now.value = Date.now() + serverOffset
  }, 250)
  poll = setInterval(() => {
    void load()
  }, 5000)
})
onBeforeUnmount(() => {
  clearInterval(tick)
  clearInterval(poll)
})
</script>

<template>
  <main class="display-page">
    <header class="display-header">
      <div class="display-title">
        <span class="display-logo">R</span>
        <div>
          <small>RCJV GAMEMASTER · TOURNAMENT LIVE</small>
          <h1>{{ workspace?.tournament.name || 'Tournament display' }}</h1>
          <span class="display-field-title">{{
            fieldRequested ? selectedField?.name || 'Field unavailable' : 'All fields'
          }}</span>
        </div>
      </div>
      <div class="wall-clock">
        <strong>{{ timeOfDay }}</strong
        ><span>{{ day }}</span>
      </div>
    </header>
    <div class="display-toolbar">
      <span class="display-live"
        ><i :class="{ stale: now - updatedAt > 20_000 }" />{{
          error ? 'Connection interrupted · retrying' : 'Live updates'
        }}</span
      >
      <div>
        <RouterLink v-if="workspace" :to="`/public/${workspace.tournament.abbrev}`"
          >Tournament board ↗</RouterLink
        >
      </div>
    </div>
    <div v-if="loading" class="display-empty">Loading tournament display…</div>
    <div v-else-if="error && !workspace" class="display-empty">
      <h2>Display unavailable</h2>
      <p>{{ error }}</p>
      <button @click="load">Try again</button>
    </div>
    <div v-else-if="fieldUnavailable" class="display-empty" role="status">
      <h2>Field unavailable</h2>
      <p>The field in this display URL is invalid or does not belong to this tournament.</p>
      <p>Open a display link from the tournament’s field settings.</p>
    </div>
    <template v-else-if="workspace">
      <section
        v-if="featured"
        class="featured-match"
        :class="{ 'match-live': featured.status === 'in_progress' }"
      >
        <header>
          <span
            >{{ leagueName(featured.league_id) }} <b>·</b> {{ stageName(featured.stage_id) }}</span
          ><strong>{{ fieldName(featured.field_id) }} <b>·</b> #{{ featured.number }}</strong>
        </header>
        <div class="featured-teams" :class="{ 'individual-run': individualRun(featured) }">
          <div
            class="featured-team"
            :class="
              featured.live_state?.blue_team_id === featured.team1_id
                ? 'side-blue'
                : featured.live_state?.blue_team_id
                  ? 'side-yellow'
                  : ''
            "
          >
            <span>{{
              individualRun(featured)
                ? 'TEAM'
                : featured.live_state?.blue_team_id
                  ? featured.live_state.blue_team_id === featured.team1_id
                    ? 'BLUE SIDE'
                    : 'YELLOW SIDE'
                  : 'HOME'
            }}</span>
            <h2>{{ firstTeamName(featured) }}</h2>
            <small v-if="featured.live_state?.kickoff_team_id === featured.team1_id">KICKOFF</small>
          </div>
          <div class="featured-centre">
            <span v-if="individualRun(featured)" class="timer-phase">INDIVIDUAL RUN</span>
            <template v-else
              ><span v-if="hasLiveScore" class="live-pill">MATCH LIVE</span
              ><strong v-if="hasLiveScore" class="live-score"
                >{{ featured.goals1 }}<span>:</span>{{ featured.goals2 }}</strong
              ><strong v-else class="versus">{{
                featured.status === 'in_progress' ? '— : —' : 'VS'
              }}</strong></template
            >
            <span class="timer-phase">{{
              individualRun(featured) ? (timer === 0 ? 'Starting soon' : 'Starts in') : phase
            }}</span
            ><strong class="display-timer" :class="{ expired: timer === 0 && hasLiveScore }">{{
              timer === null ? '— : —' : clock(timer)
            }}</strong>
          </div>
          <div
            v-if="!individualRun(featured)"
            class="featured-team"
            :class="
              featured.live_state?.blue_team_id === featured.team2_id
                ? 'side-blue'
                : featured.live_state?.blue_team_id
                  ? 'side-yellow'
                  : ''
            "
          >
            <span>{{
              featured.live_state?.blue_team_id
                ? featured.live_state.blue_team_id === featured.team2_id
                  ? 'BLUE SIDE'
                  : 'YELLOW SIDE'
                : 'AWAY'
            }}</span>
            <h2>
              {{ opponentName(featured) }}
            </h2>
            <small v-if="featured.live_state?.kickoff_team_id === featured.team2_id">KICKOFF</small>
          </div>
        </div>
      </section>
      <section v-else class="featured-match featured-empty">
        <span class="all-done">✓</span>
        <h2>
          {{ results.length ? 'All published matches complete' : 'Getting ready for kickoff' }}
        </h2>
        <p>
          {{
            results.length
              ? 'Confirmed results are shown below.'
              : 'The schedule will appear here when the organizers publish matches.'
          }}
        </p>
      </section>
      <div class="display-bottom">
        <section class="up-next">
          <header>
            <h2>Up next</h2>
            <span>{{ timezone }}</span>
          </header>
          <div v-if="!upcoming.slice(1).length" class="list-empty">
            More matches will appear as the schedule is published.
          </div>
          <div v-for="match in upcoming.slice(1, 6)" :key="match.id" class="next-match">
            <strong class="next-time">{{ scheduledTime(match) }}</strong>
            <div>
              <strong
                >{{ firstTeamName(match) }}
                <template v-if="!individualRun(match)"
                  ><span>vs</span> {{ opponentName(match) }}</template
                ></strong
              ><small>{{ leagueName(match.league_id) }} · {{ fieldName(match.field_id) }}</small>
              <small v-if="individualRun(match)">Individual run</small>
            </div>
            <span class="next-number">#{{ match.number }}</span>
          </div>
        </section>
        <section class="recent-results">
          <header>
            <h2>Latest results</h2>
            <span>Confirmed</span>
          </header>
          <div v-if="!results.length" class="list-empty">Results appear after review.</div>
          <div v-for="match in results" :key="match.id" class="result-row">
            <span>{{ teamName(match.team1_id) }}</span
            ><strong>{{
              individualRun(match) ? 'Completed' : `${match.goals1} : ${match.goals2}`
            }}</strong
            ><span>{{ opponentName(match) }}</span>
          </div>
        </section>
      </div>
      <section class="event-log">
        <header>
          <h2>Match events</h2>
          <span>Newest first · {{ fieldRequested ? selectedField?.name : 'All fields' }}</span>
        </header>
        <div v-if="!eventRows.length" class="list-empty">No recorded match events yet.</div>
        <ol v-else class="event-list">
          <li v-for="row in eventRows" :key="`${row.match.id}-${row.event.id}`" class="event-row">
            <time :datetime="row.event.at">{{ eventTime(row.event) }}</time>
            <div class="event-context">
              <strong>Match #{{ row.match.number }}</strong
              ><small
                >{{ fieldName(row.match.field_id) }} · {{ eventPhase(row.event.phase) }} ·
                {{ clock(row.event.remaining_seconds) }}</small
              >
            </div>
            <div class="event-action">
              <strong>{{ eventLabel(row.event) }}</strong
              ><span v-if="row.event.team_id" class="event-team" :class="row.event.side"
                ><i v-if="row.event.side" />{{ teamName(row.event.team_id)
                }}<small v-if="row.event.side">{{ row.event.side }}</small></span
              >
            </div>
          </li>
        </ol>
      </section>
    </template>
    <footer class="display-footer">
      <span>RCJV GameMaster</span><span>Fair play. Great robots. Better together.</span
      ><span>{{ workspace?.tournament.abbrev }}</span>
    </footer>
  </main>
</template>

<style scoped>
.display-field-title {
  display: inline-block;
  margin-top: 9px;
  font-size: 12px;
  font-weight: 700;
  color: #88c6b6;
  letter-spacing: 0.03em;
}
.event-log {
  border: 1px solid #243b4c;
  border-radius: 12px;
  overflow: hidden;
  background: #102330;
  margin-top: 24px;
}
.event-log > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 18px 22px;
  border-bottom: 1px solid #243b4c;
}
.event-log h2 {
  margin: 0;
  font-size: 13px;
  color: #bfd2dd;
  font-weight: 650;
}
.event-log header > span {
  color: #607f91;
  font-size: 9px;
}
.event-list {
  list-style: none;
  padding: 0;
  margin: 0;
  max-height: 310px;
  overflow: auto;
  scrollbar-color: #335a6b #102330;
  scrollbar-width: thin;
}
.event-row {
  display: grid;
  grid-template-columns: 86px minmax(180px, 0.8fr) minmax(200px, 1.2fr);
  gap: 20px;
  align-items: center;
  padding: 15px 22px;
  border-bottom: 1px solid #203747;
}
.event-row:last-child {
  border: 0;
}
.event-row time {
  color: #62b99f;
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}
.event-context,
.event-action {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.event-context > strong {
  color: #9ebdcb;
  font-size: 11px;
  font-weight: 650;
}
.event-context > small {
  font-size: 9px;
  color: #587d91;
}
.event-action > strong {
  color: #bcd3dc;
  font-weight: 550;
  font-size: 12px;
}
.event-team {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 10px;
  color: #7d9eae;
  overflow-wrap: anywhere;
}
.event-team i {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  display: block;
  flex-shrink: 0;
}
.event-team.blue {
  color: #77afe4;
}
.event-team.blue i {
  background: #63a5ec;
}
.event-team.yellow {
  color: #d5bd68;
}
.event-team.yellow i {
  background: #e5ca5c;
}
.event-team > small {
  font-size: 8px;
  text-transform: uppercase;
  letter-spacing: 0.07em;
  opacity: 0.7;
  margin-left: 4px;
}
@media (max-width: 600px) {
  .event-log {
    margin-top: 16px;
  }
  .event-log > header {
    padding: 16px;
  }
  .event-row {
    grid-template-columns: 60px 1fr;
    gap: 9px 12px;
    padding: 14px 16px;
  }
  .event-row time {
    font-size: 10px;
  }
  .event-context > small {
    font-size: 8px;
  }
  .event-action {
    grid-column: 2;
  }
  .event-action > strong {
    font-size: 11px;
  }
  .event-log header > span {
    font-size: 8px;
  }
  .display-field-title {
    font-size: 10px;
    margin-top: 7px;
  }
}
@media (min-width: 1800px) {
  .event-log h2 {
    font-size: 18px;
  }
  .event-log header > span {
    font-size: 12px;
  }
  .event-log > header {
    padding: 22px 28px;
  }
  .event-row {
    padding: 20px 28px;
    grid-template-columns: 110px minmax(250px, 0.8fr) minmax(250px, 1.2fr);
  }
  .event-row time {
    font-size: 17px;
  }
  .event-context > strong {
    font-size: 14px;
  }
  .event-context > small {
    font-size: 12px;
  }
  .event-action > strong {
    font-size: 16px;
  }
  .event-team {
    font-size: 13px;
  }
  .event-team > small {
    font-size: 10px;
  }
  .display-field-title {
    font-size: 16px;
  }
}
.display-page {
  background: #0d1d2b;
  color: #dbe7ef;
  min-height: 100vh;
  padding: 28px 40px;
  font-family: Inter, ui-sans-serif, system-ui, sans-serif;
  box-sizing: border-box;
}
.display-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 30px;
  padding-bottom: 20px;
  border-bottom: 1px solid #263b4c;
}
.display-title {
  display: flex;
  align-items: center;
  gap: 18px;
}
.display-logo {
  display: grid;
  place-items: center;
  width: 54px;
  height: 54px;
  background: #13aa8a;
  color: #fff;
  border-radius: 13px;
  font-weight: 900;
  font-size: 36px;
}
.display-title small {
  color: #78a39d;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.14em;
}
.display-title h1 {
  font-size: 28px;
  letter-spacing: -0.6px;
  line-height: 1.3;
  font-weight: 650;
  margin: 7px 0 0;
  color: #f4f8fb;
}
.wall-clock {
  text-align: right;
  display: grid;
  gap: 5px;
  white-space: nowrap;
}
.wall-clock strong {
  font-size: 32px;
  letter-spacing: 1px;
  font-variant-numeric: tabular-nums;
  font-weight: 550;
  color: #e5f0f6;
}
.wall-clock span {
  color: #698496;
  font-size: 11px;
}
.display-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 18px 0 22px;
}
.display-live {
  display: flex;
  align-items: center;
  gap: 9px;
  color: #6d9e93;
  font-size: 10px;
  letter-spacing: 0.02em;
}
.display-live i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #1cd39d;
  box-shadow: 0 0 0 4px #1cd39d12;
}
.display-live i.stale {
  background: #f3bd48;
  box-shadow: 0 0 0 4px #f3bd4812;
}
.display-toolbar > div {
  display: flex;
  gap: 12px;
  align-items: center;
}
.display-toolbar select,
.display-toolbar button {
  border: 1px solid #2a4354;
  border-radius: 6px;
  background: #142937;
  color: #8daab9;
  padding: 7px 11px;
  font-size: 10px;
  cursor: pointer;
  max-width: 200px;
}
.display-toolbar a {
  font-size: 10px;
  text-decoration: none;
  color: #789ba9;
  margin-left: 5px;
}
.featured-match {
  border: 1px solid #294451;
  background: radial-gradient(ellipse at 50% 100%, #1e4d4b70, transparent 70%), #132a38;
  border-radius: 16px;
  padding: 24px 32px;
  overflow: hidden;
}
.featured-match > header {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  font-size: 11px;
  color: #7b9ba9;
  padding-bottom: 20px;
  border-bottom: 1px solid #2c4656;
  text-transform: uppercase;
  letter-spacing: 0.07em;
}
.featured-match > header strong {
  color: #9fc2c8;
  font-weight: 600;
}
.featured-match > header b {
  color: #466575;
  margin: 0 6px;
  font-weight: 500;
}
.featured-teams {
  display: grid;
  grid-template-columns: 1fr minmax(200px, 0.7fr) 1fr;
  gap: 25px;
  align-items: center;
  min-height: 290px;
  padding: 25px 0 10px;
}
.featured-teams.individual-run {
  grid-template-columns: minmax(0, 1fr) minmax(110px, 0.7fr);
}
.featured-team {
  text-align: center;
  min-width: 0;
}
.featured-team > span {
  color: #70909e;
  font-size: 11px;
  letter-spacing: 0.18em;
  font-weight: 700;
}
.featured-team h2 {
  font-size: clamp(22px, 2.5vw, 42px);
  font-weight: 750;
  letter-spacing: -0.8px;
  line-height: 1.2;
  margin: 17px 0;
  overflow-wrap: anywhere;
  color: #edf6fa;
}
.featured-team small {
  color: #86bda8;
  font-size: 9px;
  letter-spacing: 0.15em;
  background: #21877322;
  padding: 6px 10px;
  border-radius: 20px;
}
.featured-team.side-blue > span {
  color: #66a9ed;
}
.featured-team.side-yellow > span {
  color: #e5cc61;
}
.featured-centre {
  display: flex;
  align-items: center;
  flex-direction: column;
  text-align: center;
}
.live-pill {
  display: inline-block;
  font-size: 9px;
  font-weight: 800;
  letter-spacing: 0.13em;
  color: #48d7b0;
  background: #1bc1971a;
  border: 1px solid #1bc19730;
  border-radius: 20px;
  padding: 6px 11px;
  margin-bottom: 10px;
}
.live-score {
  font-size: 84px;
  letter-spacing: -1px;
  color: white;
  font-variant-numeric: tabular-nums;
  line-height: 1.15;
}
.live-score span {
  color: #50717c;
  padding: 0 16px;
}
.versus {
  font-size: 54px;
  font-weight: 350;
  letter-spacing: -2px;
  color: #547781;
  margin-bottom: 16px;
}
.timer-phase {
  color: #83b9aa;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.14em;
  margin: 12px 0 5px;
}
.display-timer {
  font-size: 46px;
  color: #b8e8d5;
  letter-spacing: 1px;
  font-variant-numeric: tabular-nums;
  font-weight: 650;
}
.display-timer.expired {
  animation: displayflash 1s infinite;
}
.featured-empty {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
}
.all-done {
  color: #3dc69f;
  font-size: 50px;
}
.featured-empty h2 {
  color: #bdd6df;
  font-size: 26px;
  letter-spacing: -0.5px;
  margin: 16px 0;
}
.featured-empty p {
  color: #658c9c;
  font-size: 13px;
}
.display-bottom {
  display: grid;
  grid-template-columns: 1.2fr 1fr;
  gap: 24px;
  margin-top: 24px;
}
.display-bottom > section {
  border: 1px solid #243b4c;
  border-radius: 12px;
  overflow: hidden;
  background: #102330;
}
.display-bottom header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 18px 22px;
  border-bottom: 1px solid #243b4c;
}
.display-bottom h2 {
  margin: 0;
  font-size: 13px;
  color: #bfd2dd;
  font-weight: 650;
}
.display-bottom header > span {
  color: #607f91;
  font-size: 9px;
}
.next-match {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 16px 22px;
  border-bottom: 1px solid #203747;
}
.next-match:last-child {
  border: 0;
}
.next-time {
  color: #65bca4;
  font-size: 17px;
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  min-width: 50px;
}
.next-match > div {
  flex: 1;
  display: grid;
  gap: 6px;
}
.next-match > div > strong {
  font-weight: 550;
  color: #a7c0cf;
  font-size: 12px;
}
.next-match > div > strong > span {
  font-size: 9px;
  color: #4d7183;
  margin: 0 6px;
}
.next-match small {
  color: #55788a;
  font-size: 9px;
}
.next-number {
  color: #3d6378;
  font-size: 11px;
}
.result-row {
  display: grid;
  grid-template-columns: 1fr 65px 1fr;
  gap: 15px;
  align-items: center;
  padding: 18px 22px;
  border-bottom: 1px solid #203747;
  font-size: 11px;
  color: #92b0c0;
}
.result-row:last-child {
  border: 0;
}
.result-row > span:first-child {
  text-align: right;
}
.result-row > strong {
  text-align: center;
  color: #68b9a3;
  font-size: 17px;
  font-variant-numeric: tabular-nums;
}
.list-empty {
  padding: 36px 22px;
  color: #4f7489;
  font-size: 12px;
  text-align: center;
  line-height: 1.8;
}
.display-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding-top: 25px;
  color: #355568;
  font-size: 9px;
  letter-spacing: 0.03em;
}
.display-footer span:first-child {
  font-weight: 700;
  color: #55798c;
}
.display-footer span:last-child {
  text-transform: uppercase;
  color: #527d82;
  letter-spacing: 0.13em;
}
.display-empty {
  padding: 100px 24px;
  text-align: center;
  color: #6d93a3;
  line-height: 1.8;
}
.display-empty h2 {
  color: #a3c1cc;
}
.display-empty button {
  color: #c6e6dd;
  background: #145b4e;
  border: 1px solid #217e6c;
  border-radius: 8px;
  padding: 12px 20px;
  cursor: pointer;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
@keyframes displayflash {
  50% {
    color: #ffd269;
  }
}
@media (prefers-reduced-motion: reduce) {
  .display-timer.expired {
    animation: none;
    color: #ffd269;
  }
}
@media (min-width: 1800px) {
  .display-page {
    padding: 40px 60px;
  }
  .display-title h1 {
    font-size: 36px;
  }
  .wall-clock strong {
    font-size: 42px;
  }
  .featured-teams {
    min-height: 380px;
  }
  .live-score {
    font-size: 110px;
  }
  .display-timer {
    font-size: 64px;
  }
  .featured-team h2 {
    font-size: 52px;
  }
  .featured-match > header {
    font-size: 15px;
  }
  .display-bottom h2 {
    font-size: 18px;
  }
  .next-match {
    padding: 21px 28px;
  }
  .next-match > div > strong {
    font-size: 17px;
  }
  .next-time {
    font-size: 23px;
    min-width: 70px;
  }
  .result-row {
    font-size: 16px;
    padding: 23px 28px;
  }
  .result-row > strong {
    font-size: 25px;
  }
  .next-match small {
    font-size: 12px;
  }
  .display-bottom header {
    padding: 22px 28px;
  }
  .display-title small {
    font-size: 12px;
  }
  .timer-phase {
    font-size: 14px;
  }
  .display-toolbar {
    padding: 22px 0 26px;
  }
  .display-toolbar select,
  .display-toolbar button,
  .display-toolbar a,
  .display-live {
    font-size: 12px;
  }
}
@media (max-width: 900px) {
  .display-page {
    padding: 22px;
  }
  .display-title h1 {
    font-size: 23px;
  }
  .display-title small {
    font-size: 8px;
  }
  .display-logo {
    width: 42px;
    height: 42px;
    font-size: 28px;
    border-radius: 10px;
  }
  .wall-clock strong {
    font-size: 25px;
  }
  .wall-clock span {
    font-size: 9px;
  }
  .featured-match {
    padding: 20px;
  }
  .featured-teams {
    gap: 12px;
    grid-template-columns: 1fr 150px 1fr;
    min-height: 240px;
  }
  .featured-team h2 {
    font-size: 24px;
  }
  .live-score {
    font-size: 60px;
  }
  .live-score span {
    padding: 0 8px;
  }
  .display-timer {
    font-size: 35px;
  }
  .featured-team > span {
    font-size: 8px;
  }
  .display-bottom {
    gap: 16px;
  }
  .next-match {
    gap: 12px;
    padding: 14px;
  }
  .next-match > div > strong {
    font-size: 10px;
  }
  .next-time {
    font-size: 15px;
    min-width: 42px;
  }
  .result-row {
    padding: 16px 14px;
    font-size: 10px;
    gap: 9px;
    grid-template-columns: 1fr 55px 1fr;
  }
}
@media (max-width: 600px) {
  .display-page {
    padding: 16px;
  }
  .display-header {
    gap: 12px;
  }
  .display-title {
    gap: 11px;
  }
  .display-title h1 {
    font-size: 18px;
  }
  .display-title small {
    font-size: 7px;
    letter-spacing: 0.06em;
  }
  .display-logo {
    display: none;
  }
  .wall-clock strong {
    font-size: 21px;
  }
  .wall-clock span {
    display: none;
  }
  .display-toolbar {
    gap: 12px;
  }
  .display-toolbar > div {
    gap: 6px;
  }
  .display-toolbar button > span,
  .display-toolbar a {
    display: none;
  }
  .display-toolbar select {
    max-width: 120px;
  }
  .display-live {
    font-size: 8px;
  }
  .featured-match {
    padding: 16px;
  }
  .featured-match > header {
    font-size: 8px;
    letter-spacing: 0;
    gap: 10px;
    padding-bottom: 16px;
  }
  .featured-teams {
    grid-template-columns: 1fr 110px 1fr;
    gap: 8px;
    min-height: 200px;
    padding-top: 20px;
  }
  .featured-team h2 {
    font-size: 17px;
    margin: 12px 0;
    letter-spacing: -0.3px;
  }
  .featured-team > span {
    font-size: 7px;
    letter-spacing: 0.08em;
  }
  .live-score {
    font-size: 42px;
  }
  .live-score span {
    padding: 0 5px;
  }
  .display-timer {
    font-size: 27px;
    letter-spacing: 0;
  }
  .timer-phase {
    font-size: 7px;
    letter-spacing: 0.06em;
    line-height: 1.7;
  }
  .live-pill {
    font-size: 6px;
    padding: 5px 8px;
  }
  .versus {
    font-size: 32px;
  }
  .featured-team small {
    font-size: 7px;
    padding: 5px 7px;
  }
  .display-bottom {
    grid-template-columns: 1fr;
    margin-top: 16px;
  }
  .display-bottom header {
    padding: 16px;
  }
  .next-match {
    padding: 16px;
  }
  .next-match > div > strong {
    font-size: 12px;
  }
  .next-match small {
    font-size: 10px;
  }
  .display-footer {
    font-size: 8px;
    flex-wrap: wrap;
    gap: 10px;
  }
  .display-footer span:nth-child(2) {
    display: none;
  }
  .featured-empty {
    min-height: 250px;
  }
  .featured-empty h2 {
    font-size: 22px;
  }
  .featured-empty p {
    font-size: 12px;
    line-height: 1.6;
  }
}
</style>
