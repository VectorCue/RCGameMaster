<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '@/composables/workspace'
import { api, canEdit, formatDate } from '@/lib/api'
import type { Match, RobotCheck } from '@/lib/types'
const {
  workspace: w,
  error,
  busy,
  loading,
  perform,
  reload,
  teamName,
  leagueName,
  stageName,
} = useWorkspace()
const filter = ref('submitted'),
  selected = ref<Match | null>(null),
  goals1 = ref(0),
  goals2 = ref(0),
  notes = ref('')
const matches = computed(
  () => w.value?.matches.filter((m) => filter.value === 'all' || m.status === filter.value) ?? [],
)
const selectedLeague = computed(() =>
  w.value?.leagues.find((league) => league.id === selected.value?.league_id),
)
const editableResult = computed(
  () =>
    selected.value &&
    !selected.value.is_bye &&
    (['submitted', 'approved'].includes(selected.value.status) ||
      (!w.value?.leagues.find((l) => l.id === selected.value!.league_id)?.referee_mode &&
        ['draft', 'scheduled'].includes(selected.value.status))),
)
const pending = computed(() => w.value?.matches.filter((m) => m.status === 'submitted').length ?? 0)
function opponentName(match: Match) {
  return match.team2_id ? teamName(match.team2_id) : match.is_bye ? 'Bye' : 'Individual run'
}
function inspectionComplete(check: RobotCheck) {
  const maxWeight = selectedLeague.value?.max_weight_g ?? 0
  return (
    Number.isFinite(check.weight_g) &&
    check.weight_g > 0 &&
    (maxWeight === 0 || check.weight_g <= maxWeight) &&
    check.ball_zone &&
    check.voltage &&
    check.size &&
    check.motors
  )
}
function open(match: Match) {
  selected.value = match
  goals1.value = match.goals1
  goals2.value = match.goals2
  notes.value = match.notes
}
async function review(approve: boolean) {
  if (!selected.value) return
  if (
    await perform(() =>
      api.post(`/matches/${selected.value!.id}/review`, {
        approve,
        goals1: goals1.value,
        goals2: goals2.value,
        notes: notes.value,
      }),
    )
  )
    selected.value = null
}
</script>
<template>
  <div
    class="page-heading flex items-center justify-between gap-[20px] max-[1200px]:items-start max-[760px]:gap-[18px]"
  >
    <div>
      <p class="eyebrow">RESULT VALIDATION</p>
      <h1>
        Result review <span class="heading-count">{{ pending }}</span>
      </h1>
      <p>Check signed match reports. Approved results are published automatically.</p>
    </div>
    <div class="button-group flex flex-wrap items-center gap-[9px]">
      <button class="button" :disabled="loading" @click="reload">↻ Refresh</button
      ><select v-model="filter" aria-label="Filter match status">
        <option value="submitted">Awaiting review</option>
        <option value="approved">Approved</option>
        <option value="in_progress">In progress</option>
        <option value="all">All matches</option>
      </select>
    </div>
  </div>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <div v-if="!canEdit" class="panel empty">Editor access is required to review results.</div>
  <template v-else
    ><div v-if="loading && !w" class="loading">Loading results…</div>
    <div v-else-if="!matches.length" class="panel empty">
      <span class="empty-icon">✓</span>
      <h2>{{ filter === 'submitted' ? 'All caught up' : 'No matches here yet' }}</h2>
      <p>
        {{
          filter === 'submitted'
            ? 'Submitted match reports will appear here for your review.'
            : 'Choose another status to see more matches.'
        }}
      </p>
    </div>
    <div v-else class="panel table-container">
      <table>
        <thead>
          <tr>
            <th>Match</th>
            <th>Teams</th>
            <th>Score</th>
            <th>Stage</th>
            <th>Status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in matches" :key="m.id">
            <td>
              #{{ m.number }}<small>{{ formatDate(m.scheduled_at, w?.tournament.timezone) }}</small>
            </td>
            <td>
              <strong>{{ teamName(m.team1_id) }}</strong
              ><small>{{ opponentName(m) }}</small>
            </td>
            <td class="score-cell">{{ m.goals1 }} : {{ m.goals2 }}</td>
            <td>
              {{ leagueName(m.league_id) }}<small>{{ stageName(m.stage_id) }}</small>
            </td>
            <td>
              <span
                class="badge"
                :class="
                  m.status === 'approved' ? 'success' : m.status === 'submitted' ? 'warning' : ''
                "
                >{{ m.status.replace('_', ' ') }}</span
              >
            </td>
            <td>
              <button class="button small" @click="open(m)">
                {{ m.status === 'submitted' ? 'Review report →' : 'View / edit' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div></template
  >
  <div v-if="selected" class="modal-backdrop" @click.self="selected = null">
    <section
      class="modal wide panel"
      role="dialog"
      aria-modal="true"
      aria-labelledby="review-title"
    >
      <div class="section-title flex items-center justify-between gap-[12px]">
        <div>
          <p class="eyebrow">MATCH #{{ selected.number }}</p>
          <h2 id="review-title">Match report</h2>
        </div>
        <button class="icon-button" aria-label="Close report" @click="selected = null">×</button>
      </div>
      <div class="review-score">
        <label
          >{{ teamName(selected.team1_id)
          }}<input
            v-model.number="goals1"
            type="number"
            min="0"
            required
            :disabled="!editableResult" /></label
        ><span>:</span
        ><label
          >{{ opponentName(selected)
          }}<input
            v-model.number="goals2"
            type="number"
            min="0"
            required
            :disabled="!editableResult"
        /></label>
      </div>
      <p
        v-if="
          selected.live_state?.signed_goals1 != null && selected.live_state?.signed_goals2 != null
        "
        class="audit-notice"
      >
        Signed score:
        <strong
          >{{ selected.live_state.signed_goals1 }} : {{ selected.live_state.signed_goals2 }}</strong
        >.
        <span
          v-if="
            goals1 !== selected.live_state.signed_goals1 ||
            goals2 !== selected.live_state.signed_goals2
          "
          >The current score is an editor amendment. The original signatures remain attached to the
          signed score.</span
        >
        <span v-else>Team and referee signatures refer to this score.</span>
      </p>
      <label
        >Match notes / editor feedback<textarea
          v-model="notes"
          rows="3"
          maxlength="4000"
          :disabled="!editableResult"
        ></textarea>
      </label>
      <h3 class="form-section">Pregame inspection</h3>
      <div class="table-container overflow-x-auto">
        <table v-if="selected.live_state?.checks?.length">
          <thead>
            <tr>
              <th>Team / robot</th>
              <th>Weight</th>
              <th>Checks</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in selected.live_state?.checks ?? []" :key="`${c.team_id}-${c.robot}`">
              <td>{{ teamName(c.team_id) }} · {{ c.robot }}</td>
              <td>{{ c.weight_g }} g</td>
              <td>
                {{ inspectionComplete(c) ? 'All passed' : 'Incomplete' }}
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="!selected.live_state?.checks?.length" class="hint">
          {{
            selectedLeague?.inspections_enabled === false
              ? 'Robot inspections are disabled for this sub-league; no inspection was required.'
              : 'No robot inspection recorded.'
          }}
        </p>
      </div>
      <h3 class="form-section">Signed acceptance</h3>
      <div class="signature-review">
        <article v-for="s in selected.live_state?.signatures ?? []" :key="`${s.kind}-${s.id}`">
          <img :src="s.signature" :alt="`Signature of ${s.name}`" /><strong>{{ s.name }}</strong
          ><small>{{ s.kind }}</small>
        </article>
      </div>
      <p v-if="!selected.live_state?.signatures?.length" class="hint">No signatures recorded.</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
        <template v-if="selected.status === 'submitted'"
          ><button
            class="button primary"
            :disabled="busy || goals1 < 0 || goals2 < 0"
            @click="review(true)"
          >
            ✓ Approve & publish</button
          ><button class="button danger" :disabled="busy" @click="review(false)">
            Return for correction
          </button></template
        ><button
          v-else-if="editableResult"
          class="button primary"
          :disabled="busy || goals1 < 0 || goals2 < 0"
          @click="review(true)"
        >
          {{
            selected.status === 'approved'
              ? 'Save corrected result & publish'
              : 'Record final result & publish'
          }}</button
        ><RouterLink v-else :to="`/referee/${selected.id}`" class="button"
          >Open referee match control →</RouterLink
        ><button class="button" @click="selected = null">Close</button>
      </div>
    </section>
  </div>
</template>
