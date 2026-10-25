<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router'
import { api, errorMessage } from '@/lib/api'
import type { LiveState, Match, RobotCheck, Signature, Workspace } from '@/lib/types'
import SignaturePad from '@/components/SignaturePad.vue'

const route = useRoute()
const match = ref<Match | null>(null)
const workspace = ref<Workspace | null>(null)
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const now = ref(Date.now())
const selectedTeam = ref(0)
const selectedRobot = ref(1)
const checks = ref<RobotCheck[]>([])
const setupOpen = ref(false)
const setupPanel = ref<HTMLElement>()
const blueTeam = ref(0)
const kickoffTeam = ref(0)
const signatures = ref<Signature[]>([])
const signatureIndex = ref(0)
const notes = ref('')
let tick: ReturnType<typeof setInterval> | undefined
let poll: ReturnType<typeof setInterval> | undefined
let dirtySignatures = false
let serverOffset = 0
let loadSequence = 0

const emptyState: LiveState = {
  checks: [],
  phase: 'pregame',
  remaining_seconds: 0,
  running: false,
  started_at: null,
  blue_team_id: null,
  kickoff_team_id: null,
  timeouts: [],
  signatures: [],
}
const live = computed<LiveState>(() => ({ ...emptyState, ...match.value?.live_state }))
const league = computed(() => workspace.value?.leagues.find((l) => l.id === match.value?.league_id))
const inspectionsRequired = computed(() => league.value?.inspections_enabled !== false)
const teamIds = computed(() =>
  match.value
    ? [match.value.team1_id, match.value.team2_id].filter((id): id is number => !!id)
    : [],
)
const robots = computed(() => Math.max(1, league.value?.robot_count || 1))
const teamName = (id: number | null | undefined) =>
  workspace.value?.teams.find((t) => t.id === id)?.name || 'Team'
const selectedCheck = computed(() =>
  checks.value.find((c) => c.team_id === selectedTeam.value && c.robot === selectedRobot.value),
)
const isPregame = computed(() => live.value.phase === 'pregame')
const isLocked = computed(() => ['submitted', 'approved'].includes(match.value?.status || ''))
const isHalf = computed(() => ['first_half', 'second_half'].includes(live.value.phase))
const canChangeScore = computed(() => isHalf.value || live.value.phase === 'finished')
const remaining = computed(() => {
  const elapsed =
    live.value.running && live.value.started_at
      ? (now.value - new Date(live.value.started_at).getTime()) / 1000
      : 0
  return Math.max(0, Math.ceil(live.value.remaining_seconds - elapsed))
})
const expired = computed(
  () =>
    ['first_half', 'halftime', 'second_half'].includes(live.value.phase) && remaining.value === 0,
)
const phaseName = computed(
  () =>
    ({
      pregame: inspectionsRequired.value ? 'Robot inspection' : 'Match setup',
      ready: 'Ready for kickoff',
      first_half: 'First half',
      halftime: 'Half-time break',
      second_half: 'Second half',
      finished: 'Full time',
    })[live.value.phase],
)
const activeSignature = computed(() => signatures.value[signatureIndex.value])
const allSigned = computed(
  () =>
    (match.value?.referee_ids.length || 0) > 0 &&
    signatures.value.length >= 3 &&
    signatures.value.every((s) => !!s.signature && !!s.name.trim()),
)
const allChecksComplete = computed(
  () =>
    checks.value.length === teamIds.value.length * robots.value &&
    checks.value.every(
      (c) =>
        c.weight_g > 0 &&
        (!league.value?.max_weight_g || c.weight_g <= league.value.max_weight_g) &&
        c.ball_zone &&
        c.voltage &&
        c.size &&
        c.motors,
    ),
)
const completeCount = computed(
  () =>
    checks.value.filter(
      (c) =>
        c.weight_g > 0 &&
        (!league.value?.max_weight_g || c.weight_g <= league.value.max_weight_g) &&
        c.ball_zone &&
        c.voltage &&
        c.size &&
        c.motors,
    ).length,
)
const stageName = computed(
  () => workspace.value?.stages.find((s) => s.id === match.value?.stage_id)?.name || '',
)
const fieldName = computed(
  () =>
    workspace.value?.fields.find((f) => f.id === match.value?.field_id)?.name || 'Unassigned field',
)
function clock(seconds: number) {
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}
function timeout(team: number, robot: number) {
  return live.value.timeouts?.find((t) => t.team_id === team && t.robot === robot)
}
function timeoutRemaining(team: number, robot: number) {
  const t = timeout(team, robot)
  return t ? Math.max(0, Math.ceil((new Date(t.ends_at).getTime() - now.value) / 1000)) : null
}
function timeoutLabel(team: number, robot: number) {
  const seconds = timeoutRemaining(team, robot)
  return seconds === null
    ? '1 min timeout'
    : seconds === 0
      ? 'Ready to return · tap to reset'
      : `${clock(seconds)} · tap to reset`
}
function selectInspectionTeam(id: number) {
  selectedTeam.value = id
  selectedRobot.value = 1
}
function syncClock(header: unknown) {
  const serverTime = typeof header === 'string' ? Date.parse(header) : NaN
  if (Number.isFinite(serverTime)) serverOffset = serverTime - Date.now()
  now.value = Date.now() + serverOffset
}
function resetSignatures(includeSaved = false) {
  signatures.value = [
    ...teamIds.value.map((id) => ({
      kind: 'team' as const,
      id,
      name: teamName(id),
      signature: '',
    })),
    ...(match.value?.referee_ids ?? []).map((id) => ({
      kind: 'referee' as const,
      id,
      name: workspace.value?.referees.find((r) => r.id === id)?.name || 'Referee',
      signature: '',
    })),
  ].map((s) => ({
    ...s,
    ...(includeSaved
      ? live.value.signatures?.find((saved) => saved.kind === s.kind && saved.id === s.id)
      : undefined),
  }))
  signatureIndex.value = 0
  dirtySignatures = false
}
function syncMatch(next: Match) {
  const previous = match.value
  if (
    previous?.id === next.id &&
    new Date(previous.updated_at).getTime() > new Date(next.updated_at).getTime()
  )
    return
  const returned =
    previous && ['submitted', 'approved'].includes(previous.status) && next.status === 'in_progress'
  const scoreChanged =
    previous?.id === next.id && (previous.goals1 !== next.goals1 || previous.goals2 !== next.goals2)
  const refsChanged =
    previous?.id === next.id &&
    JSON.stringify(previous.referee_ids) !== JSON.stringify(next.referee_ids)
  match.value = next
  if (
    (returned || scoreChanged || refsChanged) &&
    live.value.phase === 'finished' &&
    !isLocked.value
  ) {
    resetSignatures()
    if (returned) notes.value = next.notes || ''
    notice.value = returned
      ? 'The editor returned this result for correction. Review the notes, correct the score if needed, and collect fresh signatures.'
      : 'The result changed. Please collect fresh signatures for the corrected score.'
  }
}
function initForms() {
  checks.value = teamIds.value.flatMap((team_id) =>
    Array.from({ length: robots.value }, (_, index) => ({
      team_id,
      robot: index + 1,
      weight_g: 0,
      ball_zone: false,
      voltage: false,
      size: false,
      motors: false,
      ...live.value.checks?.find((c) => c.team_id === team_id && c.robot === index + 1),
    })),
  )
  selectedTeam.value = teamIds.value[0] || 0
  selectedRobot.value = 1
  blueTeam.value = live.value.blue_team_id || teamIds.value[0] || 0
  kickoffTeam.value = live.value.kickoff_team_id || teamIds.value[0] || 0
  resetSignatures(isLocked.value)
  notes.value = match.value?.notes || ''
}
async function load(initial = false) {
  const id = String(route.params.id)
  const sequence = ++loadSequence
  try {
    const response = await api.get<Match>(`/matches/${id}`)
    if (id !== String(route.params.id) || sequence !== loadSequence) return
    syncClock(response.headers['x-server-time'])
    const next = response.data
    syncMatch(next)
    if (initial || !workspace.value) {
      const response = await api.get<Workspace>(`/tournaments/${next.tournament_id}/workspace`)
      if (id !== String(route.params.id) || sequence !== loadSequence) return
      workspace.value = response.data
      initForms()
    }
  } catch (err) {
    if (id === String(route.params.id) && sequence === loadSequence) error.value = errorMessage(err)
  } finally {
    if (id === String(route.params.id) && sequence === loadSequence) loading.value = false
  }
}
async function control(action: string, payload: Record<string, unknown> = {}) {
  if (busy.value || !match.value) return false
  busy.value = true
  ++loadSequence
  const id = match.value.id
  error.value = ''
  notice.value = ''
  try {
    const response = await api.post<Match>(`/matches/${id}/control`, { action, ...payload })
    if (String(id) !== String(route.params.id)) return false
    syncClock(response.headers['x-server-time'])
    syncMatch(response.data)
    return true
  } catch (err) {
    if (String(id) === String(route.params.id)) error.value = errorMessage(err)
    return false
  } finally {
    busy.value = false
  }
}
async function saveChecks() {
  if (!inspectionsRequired.value) {
    setupOpen.value = true
    return
  }
  if (await control('checks', { checks: checks.value })) setupOpen.value = true
}
async function saveProgress() {
  if (!inspectionsRequired.value) return
  if (await control('checks', { checks: checks.value })) notice.value = 'Inspection progress saved.'
}
async function setup() {
  if (await control('setup', { blue_team_id: blueTeam.value, kickoff_team_id: kickoffTeam.value }))
    setupOpen.value = false
}
async function advance() {
  await control('advance')
}
async function submit() {
  if (await control('submit', { signatures: signatures.value, notes: notes.value })) {
    dirtySignatures = false
    notice.value = 'Result submitted. An editor will review and publish the scores.'
  }
}
watch(
  signatures,
  () => {
    dirtySignatures = signatures.value.some((s) => !!s.signature)
  },
  { deep: true },
)
watch(
  () => route.params.id,
  async () => {
    ++loadSequence
    match.value = null
    workspace.value = null
    loading.value = true
    dirtySignatures = false
    setupOpen.value = false
    error.value = ''
    notice.value = ''
    await load(true)
  },
)
watch(setupOpen, async (open) => {
  if (open) {
    await nextTick()
    setupPanel.value?.querySelector<HTMLSelectElement>('select')?.focus()
  }
})
function setupKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    setupOpen.value = false
    event.preventDefault()
    return
  }
  if (event.key !== 'Tab') return
  const controls = setupPanel.value?.querySelectorAll<HTMLElement>('select, button:not(:disabled)')
  const first = controls?.[0],
    last = controls?.[controls.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    last?.focus()
    event.preventDefault()
  } else if (!event.shiftKey && document.activeElement === last) {
    first?.focus()
    event.preventDefault()
  }
}
function confirmLeave() {
  if (dirtySignatures && !isLocked.value)
    return window.confirm(
      'Leave this match? Unsaved signatures will be lost. The match timer and score remain saved.',
    )
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirtySignatures && !isLocked.value) event.preventDefault()
}
onBeforeRouteLeave(confirmLeave)
onBeforeRouteUpdate((to, from) => {
  if (to.params.id !== from.params.id) return confirmLeave()
})
onMounted(async () => {
  window.addEventListener('beforeunload', beforeUnload)
  await load(true)
  tick = setInterval(() => {
    now.value = Date.now() + serverOffset
  }, 250)
  poll = setInterval(() => {
    if (!document.hidden && !busy.value && !isPregame.value && !setupOpen.value) void load()
  }, 5000)
})
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  clearInterval(tick)
  clearInterval(poll)
})
</script>

<template>
  <div class="match-desk">
    <RouterLink class="back-link" to="/referee">← Referee desk</RouterLink>
    <p v-if="loading" class="desk-empty">Opening match…</p>
    <template v-if="match && workspace && !loading">
      <header class="desk-header">
        <div>
          <p class="eyebrow">{{ league?.name }} · {{ stageName }}</p>
          <h1>
            Match {{ match.number }} <span>{{ fieldName }}</span>
          </h1>
        </div>
        <span class="phase-chip">{{ isLocked ? match.status : phaseName }}</span>
      </header>
      <p v-if="error" class="desk-error" role="alert">{{ error }}</p>
      <p v-if="notice" class="desk-notice" role="status">{{ notice }}</p>
      <section v-if="isLocked" class="submitted-panel">
        <span class="submitted-icon">✓</span>
        <h2>
          {{
            match.status === 'approved'
              ? 'Result approved and published'
              : 'Result submitted for review'
          }}
        </h2>
        <div class="final-score">
          {{ teamName(match.team1_id) }} <strong>{{ match.goals1 }} : {{ match.goals2 }}</strong>
          {{ teamName(match.team2_id) }}
        </div>
        <p>
          {{
            inspectionsRequired
              ? 'The score, checks, and signed match sheet have been saved.'
              : 'The score and signed match sheet have been saved.'
          }}
        </p>
        <RouterLink to="/referee" class="desk-button primary">Back to referee desk</RouterLink>
      </section>
      <section v-else-if="isPregame && !inspectionsRequired" class="inspection-card setup-only">
        <p class="eyebrow">MATCH SETUP</p>
        <h2>Inspections disabled for this sub-league</h2>
        <p>Choose the teams’ starting sides and kickoff before starting the match.</p>
        <div class="setup-team-pair">
          <strong>{{ teamName(match.team1_id) }}</strong
          ><span>vs</span><strong>{{ teamName(match.team2_id) }}</strong>
        </div>
        <button class="desk-button primary" :disabled="busy" @click="setupOpen = true">
          Choose sides & kickoff →
        </button>
      </section>
      <template v-else-if="isPregame">
        <div class="inspection-intro">
          <div>
            <h2>Before the whistle</h2>
            <p>Inspect each robot against the league limits before choosing sides.</p>
          </div>
          <span>{{ completeCount }} / {{ checks.length }} robots checked</span>
        </div>
        <div class="team-tabs" role="tablist" aria-label="Inspection team">
          <button
            v-for="id in teamIds"
            :key="id"
            :class="{ active: selectedTeam === id }"
            role="tab"
            :aria-selected="selectedTeam === id"
            @click="selectInspectionTeam(id)"
          >
            {{ teamName(id) }}
          </button>
        </div>
        <div class="inspection-card">
          <div class="robot-tabs" role="tablist" aria-label="Inspection robot">
            <button
              v-for="robot in robots"
              :key="robot"
              :class="{ active: selectedRobot === robot }"
              role="tab"
              :aria-selected="selectedRobot === robot"
              @click="selectedRobot = robot"
            >
              Robot {{ robot }}
            </button>
          </div>
          <div v-if="selectedCheck" class="check-list">
            <label class="weight-check"
              ><div>
                <strong>Robot weight</strong
                ><small>{{
                  league?.max_weight_g
                    ? `Maximum ${league.max_weight_g} g`
                    : 'No maximum weight limit'
                }}</small>
              </div>
              <div class="weight-input">
                <input
                  v-model.number="selectedCheck.weight_g"
                  type="number"
                  min="1"
                  :max="league?.max_weight_g || undefined"
                  step="1"
                  aria-label="Robot weight in grams"
                /><span>g</span>
              </div></label
            >
            <p
              v-if="league?.max_weight_g && selectedCheck.weight_g > league.max_weight_g"
              class="weight-error"
            >
              This robot exceeds the maximum weight.
            </p>
            <label
              ><div>
                <strong>Ball capturing zone</strong
                ><small>{{
                  league?.ball_zone_cm
                    ? `Maximum ${league.ball_zone_cm} cm`
                    : 'Verify the configured league specification'
                }}</small>
              </div>
              <input v-model="selectedCheck.ball_zone" type="checkbox"
            /></label>
            <label
              ><div>
                <strong>Voltage</strong
                ><small>{{
                  league?.voltage
                    ? `Maximum ${league.voltage} V`
                    : 'Verify the configured league specification'
                }}</small>
              </div>
              <input v-model="selectedCheck.voltage" type="checkbox"
            /></label>
            <label
              ><div>
                <strong>Dimensions</strong
                ><small>{{
                  league?.size_cm
                    ? `Maximum ${league.size_cm} cm`
                    : 'Verify the configured league specification'
                }}</small>
              </div>
              <input v-model="selectedCheck.size" type="checkbox"
            /></label>
            <label
              ><div>
                <strong>Number of motors</strong
                ><small>{{
                  league?.motor_limit
                    ? `Maximum ${league.motor_limit} motors`
                    : 'Verify the configured league specification'
                }}</small>
              </div>
              <input v-model="selectedCheck.motors" type="checkbox"
            /></label>
          </div>
        </div>
        <footer class="inspection-footer">
          <p>All robots on both teams must pass the inspection.</p>
          <div class="inspection-actions">
            <button class="desk-button" :disabled="busy" @click="saveProgress">Save progress</button
            ><button
              class="desk-button primary"
              :disabled="busy || !allChecksComplete"
              @click="saveChecks"
            >
              {{ busy ? 'Saving…' : 'Save checks & choose sides →' }}
            </button>
          </div>
        </footer>
      </template>
      <template v-else>
        <section class="game-console" :class="{ 'timer-expired': expired }">
          <div class="scoreboard">
            <div
              v-for="id in teamIds"
              :key="id"
              class="team-score"
              :class="id === live.blue_team_id ? 'blue-team' : 'yellow-team'"
            >
              <span class="side-label"
                >{{ id === live.blue_team_id ? 'BLUE SIDE' : 'YELLOW SIDE' }}
                <b v-if="id === live.kickoff_team_id">· KICKOFF</b></span
              >
              <h2>{{ teamName(id) }}</h2>
              <strong class="score">{{
                id === match.team1_id ? match.goals1 : match.goals2
              }}</strong>
              <div v-if="canChangeScore" class="score-buttons">
                <button
                  :disabled="busy || (id === match.team1_id ? match.goals1 : match.goals2) === 0"
                  :aria-label="`Remove goal for ${teamName(id)}`"
                  @click="control('goal', { team_id: id, delta: -1 })"
                >
                  −</button
                ><button
                  class="add-goal"
                  :disabled="busy"
                  @click="control('goal', { team_id: id, delta: 1 })"
                >
                  + Goal
                </button>
              </div>
            </div>
          </div>
          <div class="clock-panel">
            <span>{{ phaseName }}</span
            ><strong class="match-clock" role="timer" aria-live="off">{{
              clock(remaining)
            }}</strong>
            <button v-if="expired" class="expiry-button" :disabled="busy" @click="advance">
              {{
                live.phase === 'first_half'
                  ? 'Half finished · tap to start half-time'
                  : live.phase === 'halftime'
                    ? 'Half-time finished · tap to prepare second half'
                    : 'Full time · tap to finish match'
              }}
            </button>
            <button
              v-else-if="live.phase !== 'finished'"
              class="desk-button timer-button"
              :disabled="busy"
              @click="control(live.running ? 'pause' : 'start')"
            >
              {{
                live.running
                  ? 'Ⅱ Pause timer'
                  : live.phase === 'ready'
                    ? '▶ Start match'
                    : live.phase === 'second_half' && remaining === league?.half_seconds
                      ? '▶ Start second half'
                      : '▶ Start timer'
              }}
            </button>
            <p v-else>Match complete. Confirm the result with signatures below.</p>
          </div>
        </section>
        <div v-if="isHalf" class="robot-timeouts">
          <section v-for="id in teamIds" :key="id">
            <h3>{{ teamName(id) }} <span>Robot timeouts</span></h3>
            <div class="timeout-grid">
              <button
                v-for="robot in robots"
                :key="robot"
                class="timeout-button"
                :class="{
                  'timeout-active': !!timeout(id, robot),
                  'timeout-expired': timeoutRemaining(id, robot) === 0,
                }"
                :disabled="busy"
                @click="control('timeout', { team_id: id, robot })"
              >
                <strong>Robot {{ robot }}</strong
                ><span>{{ timeoutLabel(id, robot) }}</span>
              </button>
            </div>
          </section>
        </div>
        <section v-if="live.phase === 'finished'" class="signing-section">
          <div class="signing-heading">
            <div>
              <h2>Accept the match result</h2>
              <p>Each team and assigned referee signs the final score before submission.</p>
            </div>
            <strong
              >{{ signatures.filter((s) => !!s.signature).length }} /
              {{ signatures.length }} signed</strong
            >
          </div>
          <p v-if="!match.referee_ids.length" class="desk-error">
            Assign at least one referee to this match before submitting the result.
          </p>
          <div class="signature-tabs">
            <button
              v-for="(signature, index) in signatures"
              :key="`${signature.kind}-${signature.id}`"
              :class="{ active: signatureIndex === index }"
              @click="signatureIndex = index"
            >
              <small>{{ signature.kind }}</small
              >{{ signature.name }} <span v-if="signature.signature">✓</span>
            </button>
          </div>
          <template v-if="activeSignature"
            ><label class="signer-name"
              >Signer name<input v-model="activeSignature.name" autocomplete="name" /></label
            ><SignaturePad
              :key="`${activeSignature.kind}-${activeSignature.id}`"
              v-model="activeSignature.signature"
              :label="`${activeSignature.kind === 'team' ? 'Team representative' : 'Referee'}: ${activeSignature.name}`" /></template
          ><label class="match-notes"
            >Match notes<textarea
              v-model="notes"
              rows="3"
              placeholder="Incidents or observations for the editor (optional)"
            />
          </label>
          <div class="submit-footer">
            <p>By signing, the participants accept the recorded match result.</p>
            <button class="desk-button primary" :disabled="busy || !allSigned" @click="submit">
              {{ busy ? 'Submitting…' : 'Submit result for review →' }}
            </button>
          </div>
        </section>
      </template>
      <div
        v-if="setupOpen"
        class="setup-overlay"
        @click.self="setupOpen = false"
        @keydown="setupKeydown"
      >
        <section
          ref="setupPanel"
          class="setup-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="setup-title"
        >
          <p class="eyebrow">COIN TOSS</p>
          <h2 id="setup-title">Choose sides & kickoff</h2>
          <p>Both switch automatically before the second half.</p>
          <label
            >Team on the blue side<select v-model="blueTeam">
              <option v-for="id in teamIds" :key="id" :value="id">{{ teamName(id) }}</option>
            </select></label
          ><label
            >Team taking the first kickoff<select v-model="kickoffTeam">
              <option v-for="id in teamIds" :key="id" :value="id">{{ teamName(id) }}</option>
            </select></label
          >
          <div class="dialog-actions">
            <button class="desk-button" :disabled="busy" @click="setupOpen = false">Back</button
            ><button class="desk-button primary" :disabled="busy" @click="setup">
              Confirm setup
            </button>
          </div>
        </section>
      </div>
    </template>
    <p v-else-if="error" class="desk-error" role="alert">{{ error }}</p>
  </div>
</template>

<style scoped>
.setup-only {
  padding: 32px;
}
.setup-only h2 {
  color: #244558;
  font-size: 21px;
  margin: 0 0 10px;
}
.setup-only > p:not(.eyebrow) {
  color: #748a9a;
  font-size: 14px;
  line-height: 1.7;
}
.setup-team-pair {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  color: #244558;
  margin: 26px 0;
}
.setup-team-pair span {
  color: #91a5b2;
  font-size: 12px;
}
.inspection-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.match-desk {
  max-width: 1100px;
  margin: 0 auto;
}
.back-link {
  display: inline-block;
  color: #668094;
  font-size: 13px;
  text-decoration: none;
  margin-bottom: 22px;
}
.desk-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 24px;
}
.eyebrow {
  font-size: 11px;
  letter-spacing: 0.12em;
  font-weight: 800;
  color: #0a9380;
  text-transform: uppercase;
  margin: 0 0 8px;
}
h1 {
  font-size: 28px;
  color: #17364b;
  margin: 0;
  letter-spacing: -0.6px;
}
h1 span {
  font-size: 15px;
  font-weight: 500;
  margin-left: 15px;
  color: #7890a0;
}
.phase-chip {
  text-transform: capitalize;
  font-size: 12px;
  background: #e2f5ef;
  color: #087963;
  padding: 8px 13px;
  border-radius: 20px;
  font-weight: 700;
}
.desk-error,
.desk-notice {
  border-radius: 8px;
  padding: 14px;
  margin: 14px 0;
}
.desk-error {
  color: #b3452b;
  background: #fff0ed;
  border: 1px solid #f3c5ba;
}
.desk-notice {
  color: #086d58;
  background: #e4f8ef;
  border: 1px solid #acdccc;
}
.desk-empty {
  padding: 60px;
  text-align: center;
  color: #7890a0;
}
.inspection-intro {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 24px;
}
.inspection-intro h2,
.signing-heading h2 {
  color: #244558;
  margin: 0 0 8px;
  font-size: 21px;
}
.inspection-intro p,
.signing-heading p {
  color: #748a9a;
  margin: 0;
  font-size: 14px;
}
.inspection-intro > span {
  color: #0c8774;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}
.team-tabs,
.signature-tabs {
  display: flex;
  gap: 8px;
  overflow: auto;
  margin-bottom: 15px;
}
.team-tabs button,
.signature-tabs button,
.robot-tabs button {
  background: white;
  border: 1px solid #d7e1e8;
  border-radius: 8px;
  padding: 13px 20px;
  color: #6a8192;
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
}
.team-tabs button {
  flex: 1;
  min-width: 180px;
}
.team-tabs button.active,
.signature-tabs button.active {
  border-color: #079784;
  background: #e9f7f2;
  color: #087f6c;
}
.inspection-card {
  background: white;
  border: 1px solid #dce6ec;
  border-radius: 12px;
  overflow: hidden;
}
.robot-tabs {
  display: flex;
  gap: 8px;
  padding: 18px 24px;
  background: #f8fafb;
  border-bottom: 1px solid #e1e8ed;
}
.robot-tabs button {
  padding: 8px 16px;
  font-size: 12px;
}
.robot-tabs button.active {
  background: #23495c;
  color: white;
  border-color: #23495c;
}
.check-list {
  padding: 0 24px;
}
.check-list > label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 21px 0;
  border-bottom: 1px solid #edf1f4;
  gap: 20px;
  cursor: pointer;
}
.check-list > label:last-child {
  border: 0;
}
.check-list label div:first-child {
  display: grid;
  gap: 6px;
}
.check-list strong {
  font-size: 15px;
  color: #25485e;
}
.check-list small {
  color: #8a9ca9;
  font-size: 12px;
}
.check-list input[type='checkbox'] {
  width: 24px;
  height: 24px;
  accent-color: #059b82;
  cursor: pointer;
  flex-shrink: 0;
}
.weight-input {
  display: flex;
  align-items: center;
  gap: 10px;
  color: #889ca9;
}
.weight-input input {
  border: 1px solid #ccdbe4;
  border-radius: 7px;
  padding: 10px;
  width: 110px;
  max-width: 30vw;
  text-align: right;
  color: #25485e;
}
.weight-error {
  color: #b6462e;
  font-size: 12px;
  margin: 6px 0;
}
.inspection-footer,
.submit-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  margin: 22px 0;
}
.inspection-footer p,
.submit-footer p {
  color: #8a9ca9;
  font-size: 12px;
}
.desk-button {
  border: 1px solid #cfdee7;
  border-radius: 8px;
  padding: 13px 20px;
  cursor: pointer;
  background: white;
  color: #2a5066;
  font-weight: 700;
  text-decoration: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}
.desk-button.primary {
  background: #079983;
  color: white;
  border-color: #079983;
}
.desk-button:disabled,
button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.game-console {
  background: #132b3e;
  border-radius: 16px;
  padding: 32px;
  color: white;
  position: relative;
}
.scoreboard {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
}
.team-score {
  text-align: center;
  padding: 22px;
  border-radius: 12px;
  min-width: 0;
}
.team-score.blue-team {
  background: #183f68;
  border-top: 4px solid #50a0ec;
}
.team-score.yellow-team {
  background: #4e4828;
  border-top: 4px solid #f3d44a;
}
.side-label {
  font-size: 10px;
  letter-spacing: 0.13em;
  color: #a8c1d7;
  font-weight: 700;
}
.yellow-team .side-label {
  color: #d8cfaa;
}
.side-label b {
  color: white;
  font-weight: 600;
}
.team-score h2 {
  font-size: 21px;
  margin: 13px 0;
  overflow-wrap: anywhere;
}
.score {
  font-size: 90px;
  line-height: 1.1;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
}
.score-buttons {
  display: flex;
  justify-content: center;
  gap: 10px;
  margin-top: 16px;
}
.score-buttons button {
  border: 1px solid #ffffff38;
  background: #ffffff12;
  color: white;
  border-radius: 8px;
  cursor: pointer;
  font-size: 24px;
  padding: 9px 20px;
  min-height: 53px;
}
.score-buttons .add-goal {
  font-size: 17px;
  font-weight: 700;
  background: #ffffff24;
  min-width: 120px;
}
.clock-panel {
  display: flex;
  align-items: center;
  flex-direction: column;
  margin-top: 25px;
  gap: 10px;
}
.clock-panel > span {
  font-size: 12px;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #9bb4c7;
}
.match-clock {
  font-size: 66px;
  line-height: 1.2;
  letter-spacing: -2px;
  font-variant-numeric: tabular-nums;
}
.timer-button {
  min-width: 200px;
  margin-top: 10px;
  background: #fff;
  color: #23465c;
  border: 0;
  font-size: 16px;
}
.clock-panel p {
  color: #a6bdcc;
  font-size: 13px;
}
.expiry-button {
  border: 1px solid #f6c74e;
  background: #f6c74e;
  color: #342e19;
  padding: 15px 24px;
  border-radius: 8px;
  font-weight: 800;
  font-size: 16px;
  cursor: pointer;
  min-height: 60px;
}
.timer-expired {
  animation: clockflash 1s infinite;
}
.robot-timeouts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
  margin: 22px 0;
}
.robot-timeouts section {
  background: white;
  border: 1px solid #dce6ec;
  border-radius: 12px;
  padding: 20px;
}
.robot-timeouts h3 {
  color: #2a4a60;
  font-size: 15px;
  margin: 0 0 14px;
  display: flex;
  justify-content: space-between;
  gap: 10px;
}
.robot-timeouts h3 span {
  color: #8da0ae;
  font-size: 11px;
  font-weight: 500;
}
.timeout-grid {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
}
.timeout-button {
  display: grid;
  gap: 7px;
  border: 1px solid #d1dfe8;
  background: #f6f9fb;
  color: #37576d;
  border-radius: 8px;
  padding: 16px 10px;
  cursor: pointer;
}
.timeout-button strong {
  font-size: 14px;
}
.timeout-button span {
  font-size: 10px;
}
.timeout-active {
  background: #fff4db;
  border-color: #e9cf88;
  color: #896710;
}
.timeout-expired {
  animation: timeoutflash 1s infinite;
}
.signing-section {
  background: white;
  border: 1px solid #dce6ec;
  border-radius: 12px;
  padding: 26px;
  margin: 24px 0;
}
.signing-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 15px;
  margin-bottom: 24px;
}
.signing-heading > strong {
  color: #088770;
  font-size: 12px;
  white-space: nowrap;
}
.signature-tabs button {
  min-width: 150px;
  text-align: left;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 5px;
  position: relative;
}
.signature-tabs small {
  text-transform: uppercase;
  letter-spacing: 0.1em;
  font-size: 9px;
  opacity: 0.7;
}
.signature-tabs button > span {
  position: absolute;
  right: 12px;
  top: 9px;
  color: #049981;
}
.signer-name,
.match-notes {
  display: grid;
  gap: 8px;
  color: #577184;
  font-size: 12px;
  font-weight: 600;
  margin: 18px 0;
}
.signer-name input,
.match-notes textarea {
  padding: 11px;
  border: 1px solid #cfdee7;
  border-radius: 7px;
  color: #2a4a60;
  font: inherit;
  font-size: 14px;
}
.submit-footer {
  margin-bottom: 0;
}
.setup-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  background: #112a41a6;
  display: grid;
  place-items: center;
  padding: 20px;
}
.setup-dialog {
  background: white;
  border-radius: 16px;
  box-shadow: 0 24px 80px #0003;
  max-width: 480px;
  width: 100%;
  padding: 32px;
}
.setup-dialog h2 {
  margin: 0 0 10px;
  color: #1b3b50;
}
.setup-dialog > p:not(.eyebrow) {
  color: #8093a1;
  font-size: 13px;
  line-height: 1.6;
}
.setup-dialog label {
  display: grid;
  gap: 8px;
  font-size: 13px;
  color: #3c5a70;
  margin-top: 22px;
  font-weight: 600;
}
.setup-dialog select {
  width: 100%;
  padding: 12px;
  border: 1px solid #cedee8;
  border-radius: 7px;
  background: white;
  color: #244a60;
}
.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 28px;
}
.submitted-panel {
  background: white;
  border: 1px solid #d8e4eb;
  border-radius: 14px;
  padding: 50px 24px;
  text-align: center;
  color: #59778b;
}
.submitted-icon {
  color: #08a086;
  background: #e4f9f0;
  border-radius: 50%;
  width: 64px;
  height: 64px;
  display: inline-grid;
  place-items: center;
  font-size: 32px;
}
.submitted-panel h2 {
  color: #244a60;
  margin: 20px 0;
}
.final-score {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 20px;
  color: #38596d;
}
.final-score strong {
  font-size: 40px;
  color: #16394e;
}
.submitted-panel p {
  font-size: 13px;
  margin: 22px 0 28px;
}
@keyframes clockflash {
  50% {
    box-shadow: 0 0 0 6px #f1bd40aa;
  }
}
@keyframes timeoutflash {
  50% {
    background: #d0f3dd;
    border-color: #31a871;
    color: #17663c;
  }
}
@media (prefers-reduced-motion: reduce) {
  .timer-expired,
  .timeout-expired {
    animation: none;
    outline: 4px solid #e3ba4b;
  }
}
@media (max-width: 650px) {
  .desk-header {
    align-items: flex-start;
  }
  h1 {
    font-size: 24px;
  }
  h1 span {
    display: block;
    margin: 8px 0 0;
    font-size: 13px;
  }
  .inspection-intro,
  .inspection-footer,
  .submit-footer,
  .signing-heading {
    flex-direction: column;
    align-items: stretch;
  }
  .game-console {
    padding: 16px;
  }
  .scoreboard {
    gap: 12px;
  }
  .team-score {
    padding: 16px 8px;
  }
  .team-score h2 {
    font-size: 16px;
    min-height: 40px;
  }
  .side-label {
    font-size: 8px;
    letter-spacing: 0.05em;
  }
  .score {
    font-size: 64px;
  }
  .score-buttons {
    gap: 6px;
  }
  .score-buttons button {
    padding: 8px 12px;
    min-width: 42px;
  }
  .score-buttons .add-goal {
    min-width: 0;
    font-size: 14px;
  }
  .match-clock {
    font-size: 60px;
  }
  .robot-timeouts {
    gap: 12px;
  }
  .robot-timeouts section {
    padding: 14px;
  }
  .robot-timeouts h3 {
    display: grid;
  }
  .timeout-grid {
    grid-template-columns: 1fr;
  }
  .check-list {
    padding: 0 16px;
  }
  .check-list strong {
    font-size: 14px;
  }
  .check-list small {
    font-size: 11px;
    max-width: 180px;
  }
  .team-tabs button {
    min-width: 130px;
    padding: 12px;
  }
  .signing-section {
    padding: 18px;
  }
  .setup-dialog {
    padding: 24px;
  }
  .expiry-button {
    font-size: 13px;
    line-height: 1.6;
  }
}
</style>
