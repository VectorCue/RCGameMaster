<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { errorMessage, formatDate, publicApi } from '@/lib/api'
import type { Match, Tournament, Workspace } from '@/lib/types'
import PublicLeagueChooser from '@/components/PublicLeagueChooser.vue'

const route = useRoute()
const router = useRouter()
const workspace = ref<Workspace | null>(null)
const tournaments = ref<Tournament[]>([])
const loading = ref(true)
const error = ref('')
function queryId(value: unknown) {
  if (typeof value !== 'string' || !/^[1-9]\d*$/.test(value)) return 0
  const id = Number(value)
  return Number.isSafeInteger(id) ? id : 0
}
const leagueId = computed(() => queryId(route.query.league))
const selectedLeague = computed(() =>
  workspace.value?.leagues.find((league) => league.id === leagueId.value),
)
const scopedLeagueIds = computed(() => {
  const selected = selectedLeague.value
  const ids = new Set<number>()
  if (!selected) return ids
  ids.add(selected.id)
  if (!selected.parent_id) {
    for (const league of workspace.value?.leagues || [])
      if (league.parent_id === selected.id) ids.add(league.id)
  }
  return ids
})
const scopedMatches = computed(() =>
  (workspace.value?.matches || []).filter((match) => scopedLeagueIds.value.has(match.league_id)),
)
const scopedFields = computed(() =>
  (workspace.value?.fields || []).filter(
    (field) =>
      scopedLeagueIds.value.has(field.league_id) ||
      field.league_id === selectedLeague.value?.parent_id,
  ),
)
const selectedContext = computed(() => {
  const league = selectedLeague.value
  if (!league) return ''
  if (league.parent_id) return `${leagueName(league.parent_id)} / ${league.name}`
  return workspace.value?.leagues.some((child) => child.parent_id === league.id)
    ? `${league.name} / All sub-leagues`
    : league.name
})
const tab = computed(() =>
  ['matches', 'standings', 'teams'].includes(String(route.query.tab))
    ? String(route.query.tab)
    : 'matches',
)
const updated = ref<Date | null>(null)
let poll: ReturnType<typeof setInterval> | undefined
let loadSequence = 0
const stages = computed(() =>
  (workspace.value?.stages ?? [])
    .filter((s) => scopedLeagueIds.value.has(s.league_id))
    .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id),
)
const stageId = computed(() => {
  const id = queryId(route.query.stage)
  return stages.value.some((stage) => stage.id === id) ? id : 0
})
const matches = computed(() =>
  scopedMatches.value
    .filter((m) => !stageId.value || m.stage_id === stageId.value)
    .sort(
      (a, b) =>
        (a.scheduled_at || '9999').localeCompare(b.scheduled_at || '9999') || a.number - b.number,
    ),
)
const teams = computed(() =>
  (workspace.value?.teams ?? [])
    .filter((t) => scopedLeagueIds.value.has(t.league_id))
    .sort((a, b) => a.name.localeCompare(b.name)),
)
const standingGroups = computed(() =>
  (workspace.value?.published_standings || [])
    .filter((publication) => scopedLeagueIds.value.has(publication.league_id))
    .flatMap((publication) => {
      const stage = workspace.value?.stages.find(
        (stage) => stage.id === publication.stage_id && stage.league_id === publication.league_id,
      )
      return stage
        ? [
            {
              stage,
              published_at: publication.published_at,
              rows: [...publication.rows].sort((a, b) => a.rank - b.rank),
            },
          ]
        : []
    }),
)
const teamName = (id: number | null) =>
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
const fieldName = (id: number | null) =>
  workspace.value?.fields.find((f) => f.id === id)?.name || 'To be assigned'
const leagueName = (id: number) => workspace.value?.leagues.find((l) => l.id === id)?.name || ''
const stageName = (id: number) => workspace.value?.stages.find((s) => s.id === id)?.name || ''
const time = (value: string | null) => formatDate(value, workspace.value?.tournament.timezone)
function matchStatus(match: Match) {
  if (match.is_placeholder) return 'Awaiting teams'
  if (match.status === 'approved') return 'Final'
  if (match.status === 'submitted' || match.live_state?.phase === 'finished') return 'Under review'
  return match.status === 'in_progress' ? 'Live' : 'Upcoming'
}
async function chooseLeague(id: number) {
  await router.push({
    name: 'public',
    params: { abbrev: route.params.abbrev },
    query: { ...route.query, league: String(id), stage: undefined },
  })
}
async function changeLeague() {
  await router.push({
    name: 'public',
    params: { abbrev: route.params.abbrev },
    query: { ...route.query, league: undefined, stage: undefined },
  })
}
async function chooseTab(value: string) {
  await router.replace({ query: { ...route.query, tab: value === 'matches' ? undefined : value } })
}
async function chooseStage(event: Event) {
  const id = (event.target as HTMLSelectElement).value
  await router.replace({ query: { ...route.query, stage: id === '0' ? undefined : id } })
}
async function load() {
  const abbrev = String(route.params.abbrev || '')
  const sequence = ++loadSequence
  if (!abbrev) {
    loading.value = false
    return
  }
  try {
    const response = await publicApi.get<Workspace>(`/${encodeURIComponent(abbrev)}`)
    if (sequence !== loadSequence || abbrev !== String(route.params.abbrev || '')) return
    workspace.value = response.data
    updated.value = new Date()
    error.value = ''
  } catch (err) {
    if (sequence === loadSequence && abbrev === String(route.params.abbrev || ''))
      error.value = errorMessage(err)
  } finally {
    if (sequence === loadSequence && abbrev === String(route.params.abbrev || ''))
      loading.value = false
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
  try {
    tournaments.value = (await publicApi.get<Tournament[]>('/tournaments')).data
  } catch {
    /* A direct tournament URL remains usable if the directory is unavailable. */
  }
  await load()
  poll = setInterval(() => {
    if (!document.hidden) void load()
  }, 15_000)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="public-page">
    <header class="public-topbar">
      <RouterLink to="/" class="public-brand"
        ><span class="brand-symbol">R</span><span>RCJV<span>GameMaster</span></span></RouterLink
      >
      <div class="topbar-links">
        <span class="public-label">PUBLIC TOURNAMENT</span
        ><RouterLink
          v-if="workspace"
          :to="`/display/${workspace.tournament.abbrev}`"
          class="display-link"
          >Screen display ↗</RouterLink
        ><RouterLink to="/login" class="signin-link">Staff sign in</RouterLink>
      </div>
    </header>
    <main class="public-content">
      <p v-if="error" class="public-error" role="alert">
        {{ error }} <button @click="load">Retry</button>
      </p>
      <div v-if="loading" class="public-empty">Loading the tournament…</div>
      <template v-else-if="workspace">
        <section class="tournament-hero">
          <div>
            <p class="public-eyebrow">THE TOURNAMENT, LIVE</p>
            <h1>{{ workspace.tournament.name }}</h1>
            <p class="tournament-dates">
              {{ workspace.tournament.start_date }} — {{ workspace.tournament.end_date }}
              <span>· {{ workspace.tournament.timezone }}</span>
            </p>
          </div>
          <div class="hero-badge">
            <span class="refresh-dot" />
            <div>
              <strong>Live tournament board</strong
              ><small>{{
                updated
                  ? `Updated ${updated.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
                  : 'Automatic updates'
              }}</small>
            </div>
          </div>
        </section>
        <template v-if="!selectedLeague"
          ><p v-if="route.query.league !== undefined" class="selection-unavailable" role="status">
            That league is not available in this tournament. Choose a league below.
          </p>
          <PublicLeagueChooser :leagues="workspace.leagues" @select="chooseLeague"
        /></template>
        <template v-else>
          <section class="selected-league-bar">
            <div>
              <span>YOUR COMPETITION</span>
              <h2>{{ selectedContext }}</h2>
            </div>
            <button @click="changeLeague">← Change league</button>
          </section>
          <div class="public-stats">
            <div>
              <strong>{{ teams.length }}</strong
              ><span>Teams</span>
            </div>
            <div>
              <strong>{{ scopedFields.length }}</strong
              ><span>Fields</span>
            </div>
            <div>
              <strong>{{ scopedMatches.length }}</strong
              ><span>Published matches</span>
            </div>
            <div>
              <strong>{{ scopedMatches.filter((m) => m.status === 'approved').length }}</strong
              ><span>Results confirmed</span>
            </div>
          </div>
          <div class="public-controls">
            <div class="public-tabs" role="tablist" aria-label="Tournament information">
              <button
                v-for="item in ['matches', 'standings', 'teams']"
                :key="item"
                :class="{ active: tab === item }"
                role="tab"
                :aria-selected="tab === item"
                @click="chooseTab(item)"
              >
                {{ item }}
              </button>
            </div>
            <div v-if="tab === 'matches'" class="public-filters">
              <label
                ><span class="sr-only">Stage</span
                ><select :value="stageId" @change="chooseStage">
                  <option :value="0">All stages</option>
                  <option v-for="stage in stages" :key="stage.id" :value="stage.id">
                    {{ scopedLeagueIds.size > 1 ? `${leagueName(stage.league_id)} · ` : ''
                    }}{{ stage.name }}
                  </option>
                </select></label
              >
            </div>
          </div>
          <section v-if="tab === 'matches'" class="public-panel">
            <header class="panel-heading">
              <h2>Match schedule</h2>
              <span>{{ matches.length }} matches</span>
            </header>
            <div v-if="!matches.length" class="public-empty">
              No published matches for this selection yet.
            </div>
            <div v-else class="public-table-wrap">
              <table class="public-table matches-table">
                <thead>
                  <tr>
                    <th>Time / Field</th>
                    <th>Match</th>
                    <th class="score-column">Score</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="match in matches" :key="match.id">
                    <td>
                      <strong>{{ time(match.scheduled_at) }}</strong
                      ><small>{{ fieldName(match.field_id) }}</small>
                    </td>
                    <td>
                      <small
                        >{{ leagueName(match.league_id) }} · {{ stageName(match.stage_id) }} · #{{
                          match.number
                        }}</small
                      >
                      <div class="public-team-pair">
                        <strong>{{ firstTeamName(match) }}</strong>
                        <small v-if="individualRun(match)">Individual run</small>
                        <template v-else
                          ><span>vs</span><strong>{{ opponentName(match) }}</strong></template
                        >
                      </div>
                    </td>
                    <td class="score-column">
                      <span v-if="individualRun(match) && match.status === 'approved'"
                        >Completed</span
                      >
                      <strong v-else-if="match.status === 'approved'" class="result-score"
                        >{{ match.goals1 }} : {{ match.goals2 }}</strong
                      ><span v-else class="unplayed">—</span>
                    </td>
                    <td>
                      <span class="public-status" :class="match.status">{{
                        matchStatus(match)
                      }}</span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
          <section v-else-if="tab === 'standings'" class="standings-panels">
            <div v-if="!standingGroups.length" class="public-panel public-empty">
              No standings published yet.
            </div>
            <article v-for="group in standingGroups" :key="group.stage.id" class="public-panel">
              <header class="panel-heading">
                <h2>
                  {{ leagueName(group.stage.league_id) }} <span>· {{ group.stage.name }}</span>
                </h2>
                <span class="standings-published"
                  >Published
                  <time :datetime="group.published_at">{{ time(group.published_at) }}</time></span
                >
              </header>
              <div v-if="!group.rows.length" class="public-empty">
                No ranked teams in this published table.
              </div>
              <div v-else class="public-table-wrap">
                <table class="public-table standings-table">
                  <thead>
                    <tr>
                      <th>#</th>
                      <th>Team</th>
                      <th title="Played">P</th>
                      <th title="Wins">W</th>
                      <th title="Draws">D</th>
                      <th title="Losses">L</th>
                      <th title="Goals for and against">GF : GA</th>
                      <th title="Goal difference">GD</th>
                      <th>Pts</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in group.rows" :key="row.team_id">
                      <td>
                        <span class="rank" :class="{ first: row.rank === 1 }">{{ row.rank }}</span>
                      </td>
                      <td>
                        <strong>{{ teamName(row.team_id) }}</strong>
                      </td>
                      <td>{{ row.played }}</td>
                      <td>{{ row.wins }}</td>
                      <td>{{ row.draws }}</td>
                      <td>{{ row.losses }}</td>
                      <td>{{ row.scored }} : {{ row.conceded }}</td>
                      <td>{{ row.difference > 0 ? '+' : '' }}{{ row.difference }}</td>
                      <td>
                        <strong class="standing-points">{{ row.points }}</strong>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </article>
          </section>
          <section v-else class="public-panel">
            <header class="panel-heading">
              <h2>The teams</h2>
              <span>{{ teams.length }} teams</span>
            </header>
            <div v-if="!teams.length" class="public-empty">No teams in this league yet.</div>
            <div v-else class="public-team-grid">
              <article v-for="team in teams" :key="team.id">
                <span class="team-monogram">{{ team.name.slice(0, 2).toUpperCase() }}</span>
                <div>
                  <strong>{{ team.name }}</strong
                  ><small>{{ team.affiliation || leagueName(team.league_id) }}</small
                  ><span
                    >{{ leagueName(team.league_id) }}
                    <template v-if="team.startnumber">· #{{ team.startnumber }}</template></span
                  >
                </div>
              </article>
            </div>
          </section>
        </template>
      </template>
      <section v-else-if="!error" class="public-panel tournament-directory">
        <h1>Explore tournaments</h1>
        <p>Select a tournament to see published schedules and results.</p>
        <button v-for="t in tournaments" :key="t.id" @click="router.push(`/public/${t.abbrev}`)">
          {{ t.name }} <span>View tournament →</span>
        </button>
        <p v-if="!tournaments.length">No public tournaments yet.</p>
      </section>
      <footer class="public-footer">
        <span>RCJV GameMaster</span
        ><span>Schedules update automatically · Standings appear when published</span>
      </footer>
    </main>
  </div>
</template>

<style scoped>
.selected-league-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin: 3px 0 20px;
}
.selected-league-bar > div > span {
  color: #0a947e;
  font-size: 9px;
  font-weight: 800;
  letter-spacing: 0.15em;
}
.selected-league-bar h2 {
  margin: 7px 0 0;
  color: #284f65;
  font-size: 21px;
  letter-spacing: -0.35px;
}
.selected-league-bar button {
  border: 1px solid #cbdde5;
  background: white;
  border-radius: 7px;
  color: #52758a;
  font-size: 11px;
  font-weight: 650;
  padding: 11px 15px;
  cursor: pointer;
  white-space: nowrap;
}
.selected-league-bar button:hover {
  border-color: #159c84;
  color: #06816c;
}
.selection-unavailable {
  color: #94743a;
  font-size: 12px;
  background: #fff6e7;
  border: 1px solid #eeddbb;
  border-radius: 9px;
  padding: 13px 17px;
  margin: 0 0 20px;
}
.standings-published {
  white-space: normal !important;
  text-align: right;
  line-height: 1.7;
}
.standings-published time {
  color: #6c9b8c;
  margin-left: 3px;
}
@media (max-width: 600px) {
  .selected-league-bar {
    align-items: flex-start;
    gap: 16px;
  }
  .selected-league-bar h2 {
    font-size: 18px;
    line-height: 1.4;
  }
  .selected-league-bar button {
    padding: 9px 11px;
    font-size: 10px;
  }
  .standings-published {
    font-size: 9px !important;
    max-width: 145px;
  }
}
.public-page {
  min-height: 100vh;
  background: #f3f6f8;
  color: #213d52;
  font-family: Inter, ui-sans-serif, system-ui, sans-serif;
}
.public-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px max(28px, calc((100vw - 1220px) / 2));
  background: white;
  border-bottom: 1px solid #dfe7ec;
}
.public-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  text-decoration: none;
  color: #18384c;
  font-size: 16px;
  font-weight: 800;
  letter-spacing: -0.5px;
  white-space: nowrap;
}
.public-brand > span:last-child {
  display: flex;
  gap: 5px;
}
.public-brand > span:last-child > span {
  font-weight: 500;
  color: #738897;
}
.brand-symbol {
  display: grid;
  place-items: center;
  background: #0c9c85;
  color: white;
  width: 34px;
  height: 34px;
  border-radius: 9px;
  font-size: 23px;
  font-weight: 900;
}
.topbar-links {
  display: flex;
  align-items: center;
  gap: 24px;
}
.public-label {
  letter-spacing: 0.12em;
  font-size: 9px;
  color: #8ca0ac;
  font-weight: 800;
}
.display-link,
.signin-link {
  color: #477084;
  font-size: 12px;
  text-decoration: none;
}
.signin-link {
  border: 1px solid #d7e2e8;
  border-radius: 7px;
  padding: 8px 13px;
}
.public-content {
  max-width: 1220px;
  padding: 38px 28px 20px;
  margin: 0 auto;
}
.tournament-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 15px 0 30px;
}
.public-eyebrow {
  color: #059581;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.18em;
  margin: 0 0 12px;
}
h1 {
  font-size: 38px;
  letter-spacing: -1.4px;
  line-height: 1.2;
  color: #1c394e;
  margin: 0 0 14px;
  font-weight: 750;
}
.tournament-dates {
  color: #8598a6;
  font-size: 13px;
  margin: 0;
}
.tournament-dates span {
  margin-left: 8px;
}
.hero-badge {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
  background: white;
  border: 1px solid #dbe6ec;
  border-radius: 12px;
  padding: 16px 20px;
}
.refresh-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #10a587;
  box-shadow: 0 0 0 5px #e4f6ef;
  flex-shrink: 0;
}
.hero-badge > div {
  display: grid;
  gap: 5px;
}
.hero-badge strong {
  font-size: 12px;
  color: #33556a;
}
.hero-badge small {
  font-size: 10px;
  color: #8ba0ae;
}
.public-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  background: white;
  border: 1px solid #dbe6ec;
  border-radius: 12px;
  padding: 22px 0;
  margin-bottom: 34px;
}
.public-stats > div {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 28px;
  border-right: 1px solid #e6edf1;
}
.public-stats > div:last-child {
  border: 0;
}
.public-stats strong {
  font-size: 28px;
  letter-spacing: -1px;
  color: #244c61;
}
.public-stats span {
  font-size: 11px;
  color: #8298a6;
}
.public-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
}
.public-tabs {
  display: flex;
  gap: 5px;
  background: #e7edf1;
  padding: 5px;
  border-radius: 8px;
}
.public-tabs button {
  border: 0;
  background: none;
  cursor: pointer;
  padding: 9px 19px;
  border-radius: 6px;
  text-transform: capitalize;
  font-weight: 650;
  font-size: 12px;
  color: #8498a7;
}
.public-tabs button.active {
  background: white;
  color: #244b61;
  box-shadow: 0 1px 3px #17354c12;
}
.public-filters {
  display: flex;
  gap: 10px;
}
.public-filters select {
  background: white;
  border: 1px solid #d4e1e9;
  border-radius: 7px;
  padding: 11px 12px;
  font-size: 12px;
  color: #537287;
  min-width: 140px;
  max-width: 240px;
}
.public-panel {
  background: white;
  border: 1px solid #dce6ec;
  border-radius: 12px;
  overflow: hidden;
}
.panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 15px;
  padding: 22px 24px;
  border-bottom: 1px solid #e5ecf1;
}
.panel-heading h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
  color: #2e5167;
}
.panel-heading h2 span {
  color: #8ca1ae;
  font-weight: 500;
}
.panel-heading > span {
  font-size: 11px;
  color: #90a3af;
  white-space: nowrap;
}
.public-table-wrap {
  overflow-x: auto;
}
.public-table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  white-space: nowrap;
}
.public-table th {
  color: #8ba0ae;
  font-size: 10px;
  font-weight: 700;
  background: #f9fbfc;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  padding: 13px 24px;
  border-bottom: 1px solid #e5edf2;
}
.public-table td {
  padding: 20px 24px;
  border-bottom: 1px solid #edf2f5;
  font-size: 12px;
  color: #6b8596;
}
.public-table tbody tr:last-child td {
  border-bottom: 0;
}
.public-table td > strong {
  color: #426278;
  font-size: 12px;
  font-weight: 600;
}
.public-table td > small {
  display: block;
  margin-top: 5px;
  font-size: 10px;
  color: #93a7b5;
}
.public-table td:nth-child(2) > small {
  margin: 0 0 8px;
  font-size: 9px;
}
.public-team-pair {
  display: flex;
  gap: 10px;
  align-items: center;
  color: #32556b;
}
.public-team-pair strong {
  font-size: 13px;
  font-weight: 650;
}
.public-team-pair span {
  font-size: 10px;
  color: #9dafbb;
}
.score-column {
  text-align: center !important;
}
.public-table .result-score {
  font-size: 18px;
  color: #234d63;
  font-variant-numeric: tabular-nums;
  letter-spacing: 2px;
}
.unplayed {
  color: #b2c2cc;
  font-size: 18px;
}
.public-status {
  display: inline-block;
  padding: 5px 10px;
  background: #eef3f6;
  color: #7f97a6;
  border-radius: 20px;
  font-size: 10px;
  font-weight: 600;
}
.public-status.approved {
  background: #e7f6ef;
  color: #07886b;
}
.public-status.in_progress {
  background: #e8f3ff;
  color: #2580c4;
}
.public-status.submitted {
  background: #fff4de;
  color: #a78229;
}
.standings-panels {
  display: grid;
  gap: 20px;
}
.standings-table th,
.standings-table td {
  padding: 15px 20px;
  text-align: center;
}
.standings-table th:nth-child(2),
.standings-table td:nth-child(2) {
  text-align: left;
  min-width: 190px;
}
.rank {
  display: inline-grid;
  place-items: center;
  width: 28px;
  height: 28px;
  background: #f0f5f8;
  border-radius: 7px;
  color: #758e9f;
  font-size: 11px;
  font-weight: 700;
}
.rank.first {
  color: #0a977b;
  background: #e5f8ef;
}
.standing-points {
  color: #118c74;
  font-size: 15px;
}
.public-team-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  padding: 24px;
  gap: 18px;
}
.public-team-grid article {
  display: flex;
  gap: 13px;
  align-items: center;
  padding: 17px;
  border: 1px solid #e0e9ef;
  border-radius: 9px;
  min-width: 0;
}
.team-monogram {
  width: 43px;
  height: 43px;
  border-radius: 10px;
  background: #eaf4f3;
  color: #22937f;
  display: grid;
  place-items: center;
  font-size: 13px;
  font-weight: 800;
  flex-shrink: 0;
}
.public-team-grid article > div {
  display: grid;
  gap: 5px;
  min-width: 0;
}
.public-team-grid strong {
  font-size: 13px;
  color: #34566b;
  overflow-wrap: anywhere;
}
.public-team-grid small {
  color: #8ba0ae;
  font-size: 11px;
}
.public-team-grid article > div > span {
  color: #9aaeba;
  font-size: 9px;
}
.public-empty {
  padding: 60px 24px;
  text-align: center;
  color: #94a6b2;
  font-size: 13px;
}
.public-footer {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  margin-top: 30px;
  padding: 14px 0;
  color: #9fb0bb;
  font-size: 10px;
}
.public-footer span:first-child {
  font-weight: 700;
  color: #839aa9;
}
.public-error {
  background: #fff0ed;
  border: 1px solid #f0c9c0;
  border-radius: 9px;
  color: #a94e38;
  font-size: 13px;
  padding: 15px;
  display: flex;
  justify-content: space-between;
  gap: 20px;
}
.public-error button {
  background: transparent;
  border: 0;
  text-decoration: underline;
  color: inherit;
  cursor: pointer;
}
.tournament-directory {
  padding: 30px;
}
.tournament-directory > p {
  color: #7f96a4;
  font-size: 14px;
}
.tournament-directory > button {
  width: 100%;
  padding: 18px 0;
  display: flex;
  justify-content: space-between;
  background: transparent;
  border: 0;
  border-bottom: 1px solid #e0e9ef;
  color: #32576b;
  cursor: pointer;
  text-align: left;
  font-weight: 700;
}
.tournament-directory > button span {
  font-size: 12px;
  color: #0a9b82;
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
@media (max-width: 900px) {
  .public-label {
    display: none;
  }
  .public-stats > div {
    padding: 0 18px;
    gap: 8px;
  }
  .public-stats span {
    font-size: 10px;
  }
  .public-stats strong {
    font-size: 24px;
  }
  .public-team-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .hero-badge {
    padding: 14px;
  }
  .tournament-hero h1 {
    font-size: 32px;
  }
  .public-controls {
    flex-wrap: wrap;
  }
  .public-table td,
  .public-table th {
    padding: 16px;
  }
  .public-content {
    padding: 28px 20px;
  }
  .public-topbar {
    padding: 18px 20px;
  }
  .topbar-links {
    gap: 14px;
  }
}
@media (max-width: 600px) {
  .public-brand > span:last-child > span {
    display: none;
  }
  .display-link {
    font-size: 10px;
  }
  .signin-link {
    font-size: 10px;
    padding: 7px 9px;
  }
  .public-topbar {
    padding: 16px;
  }
  .tournament-hero {
    align-items: flex-start;
    flex-direction: column;
    gap: 22px;
  }
  .tournament-hero h1 {
    font-size: 30px;
  }
  .hero-badge {
    padding: 12px 16px;
  }
  .hero-badge small {
    display: none;
  }
  .public-stats {
    grid-template-columns: repeat(2, 1fr);
    padding: 15px 0;
    gap: 18px;
  }
  .public-stats > div:nth-child(2) {
    border: 0;
  }
  .public-content {
    padding: 24px 16px;
  }
  .public-controls {
    gap: 14px;
  }
  .public-filters {
    width: 100%;
  }
  .public-filters label {
    flex: 1;
    min-width: 0;
  }
  .public-filters select {
    min-width: 0;
    width: 100%;
    max-width: none;
    font-size: 11px;
  }
  .public-tabs {
    width: 100%;
  }
  .public-tabs button {
    flex: 1;
  }
  .public-footer {
    flex-direction: column;
    gap: 9px;
    font-size: 9px;
  }
  .public-team-grid {
    grid-template-columns: 1fr;
    padding: 16px;
    gap: 10px;
  }
  .public-team-pair {
    display: grid;
    gap: 4px;
  }
  .public-team-pair span {
    font-size: 9px;
  }
  .panel-heading {
    padding: 18px;
  }
  .panel-heading > span {
    font-size: 9px;
  }
  .public-table td,
  .public-table th {
    padding: 14px;
  }
  .public-table th {
    font-size: 9px;
  }
  .public-table td > strong {
    font-size: 10px;
  }
  .public-table td > small {
    font-size: 9px;
  }
  .public-team-pair strong {
    font-size: 12px;
  }
  .public-status {
    font-size: 9px;
  }
  .panel-heading h2 {
    font-size: 13px;
  }
}
</style>
