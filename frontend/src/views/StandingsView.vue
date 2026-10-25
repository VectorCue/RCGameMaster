<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useWorkspace } from '@/composables/workspace'
import { api, canEdit, formatDate } from '@/lib/api'
import type { League, PublishedStandings, Stage, Standing } from '@/lib/types'

const route = useRoute(),
  router = useRouter()
const {
  workspace: w,
  tournamentId,
  loading,
  error,
  busy,
  perform,
  reload,
  teamName,
} = useWorkspace()
const leagueId = ref(0),
  stageId = ref(0),
  notice = ref('')
const mainLeagues = computed(() => w.value?.leagues.filter((league) => !league.parent_id) ?? [])
const selectedLeague = computed(() =>
  w.value?.leagues.find((league) => league.id === leagueId.value),
)
const leagueStages = computed(
  () =>
    w.value?.stages
      .filter((stage) => stage.league_id === leagueId.value)
      .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id) ?? [],
)
const selectedStage = computed(() => leagueStages.value.find((stage) => stage.id === stageId.value))
const autoPublish = computed(() => selectedLeague.value?.standings_auto_publish !== false)
const publications = computed(() =>
  (w.value?.published_standings ?? []).filter(
    (publication) => publication.league_id === leagueId.value,
  ),
)
const selectedPublication = computed(() =>
  publications.value.find((publication) => publication.stage_id === stageId.value),
)
const latestPublication = computed(() => {
  for (const stage of [...leagueStages.value].reverse()) {
    const publication = publications.value.find((item) => item.stage_id === stage.id)
    if (publication) return publication
  }
  return undefined
})
const liveRows = computed(
  () =>
    w.value?.standings
      .filter((row) => row.league_id === leagueId.value && row.stage_id === stageId.value)
      .sort((a, b) => a.rank - b.rank) ?? [],
)
function eligibleResults(id: number) {
  return (
    w.value?.matches.filter(
      (match) =>
        match.stage_id === id &&
        match.status === 'approved' &&
        match.published &&
        !match.is_bye &&
        !match.is_placeholder &&
        match.team1_id > 0,
    ).length ?? 0
  )
}
const latestEligibleStage = computed(() =>
  [...leagueStages.value].reverse().find((stage) => eligibleResults(stage.id) > 0),
)
const tablePanels = computed(() => [
  {
    key: 'live',
    title: 'Live standings',
    rows: liveRows.value,
    description: 'Calculated from approved games, including games whose schedules are not public.',
    empty: 'No live rows yet. Register teams and complete games to build this table.',
  },
  {
    key: 'published',
    title: 'Published snapshot',
    rows: [...(selectedPublication.value?.rows ?? [])].sort((a, b) => a.rank - b.rank),
    description: selectedPublication.value
      ? `${autoPublish.value ? 'Auto publication' : 'Frozen manual publication'} · ${publicationTime(selectedPublication.value)}`
      : 'This stage has no published snapshot.',
    empty:
      'No published rows for this stage. Only approved, published real results can be published.',
  },
])
function stageName(id: number) {
  return w.value?.stages.find((stage) => stage.id === id)?.name ?? 'Stage'
}
function publicationTime(publication: PublishedStandings) {
  return formatDate(publication.published_at, w.value?.tournament.timezone, { second: '2-digit' })
}
function defaultStage() {
  return (
    latestPublication.value?.stage_id ??
    latestEligibleStage.value?.id ??
    leagueStages.value[0]?.id ??
    0
  )
}
function chooseLeague(league: League) {
  leagueId.value = league.id
  stageId.value = defaultStage()
  notice.value = ''
  error.value = ''
  void router.replace({
    query: {
      ...route.query,
      league: String(league.id),
      ...(stageId.value ? { stage: String(stageId.value) } : { stage: undefined }),
    },
  })
}
function chooseStage(stage: Stage) {
  stageId.value = stage.id
  notice.value = ''
  error.value = ''
  void router.replace({
    query: { ...route.query, league: String(leagueId.value), stage: String(stage.id) },
  })
}
function applyQuery() {
  const league = w.value?.leagues.find((item) => item.id === Number(route.query.league))
  if (league) leagueId.value = league.id
  const stage = leagueStages.value.find((item) => item.id === Number(route.query.stage))
  if (stage) stageId.value = stage.id
  else if (!selectedStage.value) stageId.value = defaultStage()
}
watch(() => [route.query.league, route.query.stage], applyQuery)
watch(w, (workspace) => {
  if (!workspace) return
  if (!workspace.leagues.some((league) => league.id === leagueId.value)) {
    leagueId.value =
      workspace.leagues.find((league) => league.id === Number(route.query.league))?.id ??
      workspace.leagues.find(
        (league) =>
          league.parent_id && workspace.stages.some((stage) => stage.league_id === league.id),
      )?.id ??
      workspace.leagues.find((league) =>
        workspace.stages.some((stage) => stage.league_id === league.id),
      )?.id ??
      workspace.leagues[0]?.id ??
      0
  }
  applyQuery()
  if (!selectedStage.value) stageId.value = defaultStage()
})
watch(tournamentId, () => {
  leagueId.value = 0
  stageId.value = 0
  notice.value = ''
})
async function setAutomatic(event: Event) {
  const input = event.target as HTMLInputElement
  const enabled = input.checked,
    league = selectedLeague.value
  if (!league || !canEdit.value) {
    input.checked = autoPublish.value
    return
  }
  notice.value = ''
  const saved = await perform(() =>
    api.put(`/leagues/${league.id}/standings-settings`, { auto_publish: enabled }),
  )
  input.checked = autoPublish.value
  if (saved && !error.value)
    notice.value = enabled
      ? `Automatic publishing enabled for ${league.name}. The newest eligible stage is selected; empty future stages cannot replace it.`
      : `Automatic publishing disabled for ${league.name}. Current published tables are frozen until you republish them.`
}
async function publishStandings() {
  const stage = selectedStage.value
  if (!stage || !canEdit.value || !eligibleResults(stage.id)) return
  notice.value = ''
  if ((await perform(() => api.post(`/stages/${stage.id}/standings/publish`, {}))) && !error.value)
    notice.value = `Standings published for ${stage.name}. The snapshot contains approved, published results.`
}
async function unpublishStandings() {
  const stage = selectedStage.value
  if (!stage || !canEdit.value || autoPublish.value || !selectedPublication.value) return
  notice.value = ''
  if (
    (await perform(() => api.delete(`/stages/${stage.id}/standings/publication`))) &&
    !error.value
  )
    notice.value = `Standings unpublished for ${stage.name}. The live table remains available here.`
}
function difference(row: Standing) {
  return row.difference > 0 ? `+${row.difference}` : row.difference
}
</script>
<template>
  <div
    class="page-heading flex items-center justify-between gap-[20px] max-[1200px]:items-start max-[760px]:gap-[18px]"
  >
    <div>
      <p class="eyebrow">RANKINGS & PUBLICATION</p>
      <h1>Standings</h1>
      <p>Review live tables and choose which results your tournament publishes.</p>
    </div>
    <button type="button" class="button" :disabled="busy || loading" @click="reload">
      ↻ Refresh
    </button>
  </div>
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <p
    v-if="notice"
    role="status"
    class="mb-[20px] rounded-[8px] border border-[#d7e8c6] bg-[#f2f8e9] px-[16px] py-[12px] text-[12px] text-[#4d7139]"
  >
    {{ notice }}
  </p>
  <div v-if="loading && !w" class="loading">Loading standings…</div>
  <template v-if="w">
    <section v-if="!w.leagues.length" class="panel empty">
      <span class="empty-icon">≡</span>
      <h2>Add your leagues first</h2>
      <p>Create a main league and its sub-leagues, then add stages and teams.</p>
      <RouterLink :to="`/tournaments/${tournamentId}/setup`" class="button primary"
        >Tournament setup →</RouterLink
      >
    </section>
    <div
      v-else
      class="grid grid-cols-[220px_minmax(0,1fr)] items-start gap-[22px] max-[1000px]:grid-cols-1"
    >
      <aside class="overflow-hidden rounded-[10px] border border-[color:var(--border)] bg-white">
        <h2
          id="standings-leagues-title"
          class="m-0 bg-[#203b58] px-[18px] py-[14px] text-[15px] font-semibold text-white"
        >
          Leagues
        </h2>
        <nav aria-labelledby="standings-leagues-title" class="max-h-[560px] overflow-y-auto">
          <div v-for="main in mainLeagues" :key="main.id">
            <button
              type="button"
              class="flex w-full items-center justify-between gap-[12px] border-b border-[#edf0f4] px-[18px] py-[14px] text-left text-[12px] font-semibold hover:bg-[#f4f7ee]"
              :class="
                leagueId === main.id ? 'bg-[#edf4df] text-[#3e6228]' : 'bg-[#fafbfd] text-ink'
              "
              :disabled="busy"
              :aria-pressed="leagueId === main.id"
              @click="chooseLeague(main)"
            >
              <span>{{ main.name }}</span
              ><span aria-hidden="true">›</span></button
            ><button
              v-for="league in w.leagues.filter((item) => item.parent_id === main.id)"
              :key="league.id"
              type="button"
              class="flex w-full items-center justify-between gap-[12px] border-b border-[#edf0f4] py-[13px] pl-[30px] pr-[18px] text-left text-[12px] hover:bg-[#f4f7ee]"
              :class="
                leagueId === league.id
                  ? 'bg-[#edf4df] font-semibold text-[#3e6228]'
                  : 'bg-white text-[#596b80]'
              "
              :disabled="busy"
              :aria-pressed="leagueId === league.id"
              @click="chooseLeague(league)"
            >
              <span>{{ league.name }}</span
              ><span aria-hidden="true">›</span>
            </button>
          </div>
        </nav>
      </aside>
      <div v-if="selectedLeague" class="min-w-0">
        <section class="panel">
          <div class="section-title">
            <div>
              <p class="eyebrow">
                {{
                  selectedLeague.parent_id
                    ? w.leagues.find((league) => league.id === selectedLeague?.parent_id)?.name
                    : 'MAIN LEAGUE'
                }}
              </p>
              <h2>{{ selectedLeague.name }}</h2>
            </div>
            <RouterLink
              :to="{
                name: 'public',
                params: { abbrev: w.tournament.abbrev },
                query: { league: selectedLeague.id, tab: 'standings' },
              }"
              class="button small"
              >Public standings ↗</RouterLink
            >
          </div>
          <div class="rounded-[9px] bg-[#f5f8ee] p-[16px]" :aria-busy="busy">
            <label class="checkbox"
              ><input
                type="checkbox"
                role="switch"
                aria-label="Auto publish latest"
                :checked="autoPublish"
                :disabled="busy || !canEdit"
                @change="setAutomatic"
              /><span class="text-[12px] font-semibold">Auto publish latest</span
              ><span class="badge" :class="autoPublish ? 'success' : ''">{{
                autoPublish ? 'On' : 'Manual'
              }}</span></label
            >
            <p class="hint mt-[12px]">
              {{
                autoPublish
                  ? 'Automatically publishes the newest stage with an approved, published real result. Future empty stages cannot replace the current table.'
                  : 'Published rows stay frozen when results change. Publish again to refresh a stage, or unpublish its snapshot.'
              }}
            </p>
            <p v-if="!autoPublish" class="hint mt-[8px]">
              If a source result is retracted or unpublished, its publication is withdrawn until the
              standings can be published again.
            </p>
            <p
              v-if="w.leagues.some((league) => league.parent_id === selectedLeague?.id)"
              class="hint mt-[8px]"
            >
              This controls the main league's own stages. Each sub-league has its own publishing
              setting.
            </p>
          </div>
          <div class="mt-[18px] rounded-[8px] border border-[color:var(--border)] p-[14px]">
            <p class="mb-[7px] text-[12px]">
              <strong>Current public table:</strong>
              {{
                latestPublication
                  ? stageName(latestPublication.stage_id)
                  : 'No published standings yet'
              }}
            </p>
            <p v-if="latestPublication" class="hint">
              Published {{ publicationTime(latestPublication) }}
            </p>
            <p v-else class="hint">
              A stage needs at least one approved, published match or individual run. Automatic byes
              and empty slots do not qualify.
            </p>
            <p v-if="autoPublish && latestEligibleStage" class="hint mt-[7px]">
              Newest eligible stage: {{ latestEligibleStage.name }}.
            </p>
          </div>
        </section>
        <section v-if="!leagueStages.length" class="panel empty">
          <h2>No stages in this league</h2>
          <p>
            {{
              selectedLeague.parent_id
                ? 'Add a stage to review and publish its standings.'
                : 'Choose a sub-league to manage that competition, or add a stage directly to this league.'
            }}
          </p>
          <RouterLink :to="`/tournaments/${tournamentId}/setup`" class="button"
            >Tournament setup →</RouterLink
          >
        </section>
        <template v-else>
          <nav aria-label="Choose standings stage" class="my-[20px] flex flex-wrap gap-[10px]">
            <button
              v-for="stage in leagueStages"
              :key="stage.id"
              type="button"
              class="rounded-[8px] border px-[14px] py-[11px] text-left"
              :class="
                stageId === stage.id
                  ? 'border-[#bed491] bg-[#f2f7e9] text-[#3e6228]'
                  : 'border-[#dfe6ee] bg-white text-[#3c526c]'
              "
              :disabled="busy"
              :aria-pressed="stageId === stage.id"
              @click="chooseStage(stage)"
            >
              <strong class="block text-[12px] font-medium">{{ stage.name }}</strong
              ><small class="mt-[5px] block text-[9px] text-muted"
                >{{
                  publications.some((publication) => publication.stage_id === stage.id)
                    ? 'Published'
                    : 'Unpublished'
                }}
                · {{ eligibleResults(stage.id) }} eligible
                {{ eligibleResults(stage.id) === 1 ? 'result' : 'results' }}</small
              >
            </button>
          </nav>
          <template v-if="selectedStage">
            <section class="panel">
              <div class="section-title">
                <div>
                  <h2>{{ selectedStage.name }}</h2>
                  <p>
                    {{
                      selectedPublication
                        ? `Published ${publicationTime(selectedPublication)}`
                        : 'No published snapshot for this stage.'
                    }}
                  </p>
                </div>
                <span class="badge" :class="selectedPublication ? 'success' : 'warning'">{{
                  selectedPublication ? 'Published' : 'Unpublished'
                }}</span>
              </div>
              <div v-if="canEdit" class="button-group">
                <button
                  type="button"
                  class="button primary"
                  :disabled="busy || !eligibleResults(selectedStage.id)"
                  @click="publishStandings"
                >
                  {{ selectedPublication ? 'Republish standings' : 'Publish standings' }}</button
                ><button
                  v-if="!autoPublish && selectedPublication"
                  type="button"
                  class="button danger"
                  :disabled="busy"
                  @click="unpublishStandings"
                >
                  Unpublish standings
                </button>
              </div>
              <p class="hint mt-[14px]">
                {{
                  eligibleResults(selectedStage.id)
                    ? 'Publishing captures approved, published results. A stage can be republished to replace its saved table.'
                    : 'Publish becomes available after at least one real result is both approved and public. Future empty stages and automatic byes cannot be promoted.'
                }}
              </p>
              <p v-if="!canEdit" class="hint mt-[8px]">
                An administrator or editor can change publishing settings.
              </p>
            </section>
            <div class="mt-[20px] grid grid-cols-1 gap-[20px] min-[1600px]:grid-cols-2">
              <section
                v-for="panel in tablePanels"
                :key="panel.key"
                class="panel !mt-0 min-w-0"
                :aria-labelledby="`standings-${panel.key}-title`"
              >
                <div class="section-title">
                  <h3 :id="`standings-${panel.key}-title`">{{ panel.title }}</h3>
                  <span class="badge">{{ panel.rows.length }} teams</span>
                </div>
                <p class="hint mb-[16px]">{{ panel.description }}</p>
                <div v-if="!panel.rows.length" class="empty compact">{{ panel.empty }}</div>
                <div v-else class="table-container">
                  <table class="standings-table">
                    <thead>
                      <tr>
                        <th scope="col">Rank</th>
                        <th scope="col">Team</th>
                        <th scope="col" title="Played">P</th>
                        <th scope="col" title="Wins">W</th>
                        <th scope="col" title="Draws">D</th>
                        <th scope="col" title="Losses">L</th>
                        <th scope="col" title="Goals for">GF</th>
                        <th scope="col" title="Goals against">GA</th>
                        <th scope="col" title="Goal difference">GD</th>
                        <th scope="col">Points</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="row in panel.rows" :key="row.team_id">
                        <td>{{ row.rank }}</td>
                        <th scope="row">{{ teamName(row.team_id) }}</th>
                        <td>{{ row.played }}</td>
                        <td>{{ row.wins }}</td>
                        <td>{{ row.draws }}</td>
                        <td>{{ row.losses }}</td>
                        <td>{{ row.scored }}</td>
                        <td>{{ row.conceded }}</td>
                        <td>{{ difference(row) }}</td>
                        <td class="font-semibold">{{ row.points }}</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>
            </div>
            <div class="mt-[18px] flex flex-wrap gap-[14px]">
              <RouterLink
                :to="{
                  name: 'stage-games',
                  params: { id: tournamentId },
                  query: { stage: selectedStage.id },
                }"
                class="text-button"
                >Stage games →</RouterLink
              ><RouterLink
                v-if="canEdit"
                :to="`/tournaments/${tournamentId}/review`"
                class="text-button"
                >Review results →</RouterLink
              >
            </div>
          </template>
        </template>
      </div>
    </div>
  </template>
</template>
<style scoped>
.standings-table th,
.standings-table td {
  padding: 12px 10px;
}
.standings-table tbody th {
  background: white;
  text-transform: none;
  letter-spacing: normal;
  font-size: 11px;
  color: #40546b;
}
</style>
