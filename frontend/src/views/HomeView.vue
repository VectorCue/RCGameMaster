<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  activeTournament,
  activeTournamentId,
  api,
  canAdmin,
  errorMessage,
  loadTournaments,
  selectTournament,
  tournaments,
} from '@/lib/api'
import type { Tournament, Workspace } from '@/lib/types'
const router = useRouter()
const error = ref(''),
  loading = ref(true),
  creating = ref(false),
  showForm = ref(false)
const workspace = ref<Workspace | null>(null)
const today = new Date().toISOString().slice(0, 10)
const form = ref({
  name: '',
  abbrev: '',
  start_date: today,
  end_date: today,
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
})
const submitted = computed(
  () => workspace.value?.matches.filter((m) => m.status === 'submitted').length ?? 0,
)
const scheduled = computed(() => workspace.value?.matches.filter((m) => m.slot_id).length ?? 0)
async function load() {
  loading.value = true
  try {
    await loadTournaments()
    if (activeTournamentId.value)
      workspace.value = (
        await api.get<Workspace>(`/tournaments/${activeTournamentId.value}/workspace`)
      ).data
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}
async function create() {
  creating.value = true
  error.value = ''
  try {
    const t = (await api.post<Tournament>('/tournaments', form.value)).data
    selectTournament(t.id)
    await loadTournaments()
    await router.push(`/tournaments/${t.id}/setup`)
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    creating.value = false
  }
}
async function open(t: Tournament) {
  selectTournament(t.id)
  await router.push(`/tournaments/${t.id}/schedule`)
}
onMounted(load)
</script>
<template>
  <div
    class="page-heading flex items-center justify-between gap-[20px] max-[1200px]:items-start max-[760px]:gap-[18px]"
  >
    <div>
      <p class="eyebrow">CONTROL CENTER</p>
      <h1>Tournament overview</h1>
      <p>Everything your event needs, from the first pairing to the final whistle.</p>
    </div>
    <button v-if="canAdmin" class="button primary" @click="showForm = !showForm">
      ＋ New tournament
    </button>
  </div>
  <p v-if="error" role="alert" class="error">{{ error }}</p>
  <form v-if="showForm && canAdmin" class="panel form-grid" @submit.prevent="create">
    <h2 class="full-width">Create a tournament</h2>
    <label
      >Tournament name<input
        v-model="form.name"
        required
        placeholder="RoboCup Junior 2027"
        maxlength="150" /></label
    ><label
      >Public abbreviation<input
        v-model="form.abbrev"
        required
        pattern="[A-Za-z0-9_-]+"
        placeholder="RCJ2027"
        maxlength="40" /></label
    ><label>First day<input v-model="form.start_date" required type="date" /></label
    ><label
      >Last day<input v-model="form.end_date" :min="form.start_date" required type="date" /></label
    ><label>Timezone<input v-model="form.timezone" required placeholder="Europe/Berlin" /></label>
    <div class="form-actions col-span-full flex flex-wrap gap-[10px]">
      <button class="button primary" :disabled="creating">
        {{ creating ? 'Creating…' : 'Create tournament' }}</button
      ><button type="button" class="button" @click="showForm = false">Cancel</button>
    </div>
  </form>
  <div v-if="loading" class="loading">Loading your workspace…</div>
  <template v-else>
    <section v-if="activeTournament" class="hero-panel">
      <div>
        <span class="badge light">ACTIVE TOURNAMENT</span>
        <h2>{{ activeTournament.name }}</h2>
        <p>
          {{ activeTournament.start_date }} — {{ activeTournament.end_date }}
          <span class="separator">·</span> {{ activeTournament.timezone }}
        </p>
        <RouterLink :to="`/tournaments/${activeTournament.id}/schedule`" class="button primary"
          >Open match schedule <span>↗</span></RouterLink
        >
      </div>
      <div class="hero-art" aria-hidden="true">
        <span class="hero-ball">✣</span><span class="orbit one"></span
        ><span class="orbit two"></span><span class="hero-tag">READY. SET. ROBOT.</span>
      </div>
    </section>
    <div class="stats-grid">
      <article class="stat-card">
        <span class="stat-icon">⚑</span>
        <div>
          <strong>{{ workspace?.teams.length ?? 0 }}</strong
          ><span>Registered teams</span>
        </div>
      </article>
      <article class="stat-card">
        <span class="stat-icon">▦</span>
        <div>
          <strong
            >{{ scheduled }}<small> / {{ workspace?.matches.length ?? 0 }}</small></strong
          ><span>Matches scheduled</span>
        </div>
      </article>
      <article class="stat-card">
        <span class="stat-icon">▣</span>
        <div>
          <strong>{{ workspace?.fields.length ?? 0 }}</strong
          ><span>Competition fields</span>
        </div>
      </article>
      <article class="stat-card">
        <span class="stat-icon amber">✓</span>
        <div>
          <strong>{{ submitted }}</strong
          ><span>Results awaiting review</span>
        </div>
      </article>
    </div>
    <div class="section-title flex items-center justify-between gap-[12px]">
      <div>
        <h2>Your tournaments</h2>
        <p>Select an event to manage its leagues, teams and matches.</p>
      </div>
      <span class="badge">{{ tournaments.length }} events</span>
    </div>
    <div v-if="!tournaments.length" class="empty panel">
      <span class="empty-icon">⚑</span>
      <h2>Your first tournament starts here</h2>
      <p>Create an event, add your leagues and teams, then build a schedule.</p>
      <button v-if="canAdmin" class="button primary" @click="showForm = true">
        Create tournament
      </button>
      <p v-else>An administrator can create your first event.</p>
    </div>
    <div v-else class="tournament-grid">
      <article v-for="t in tournaments" :key="t.id" class="panel tournament-card">
        <div class="row-between">
          <span class="tournament-monogram">{{ t.abbrev.slice(0, 2).toUpperCase() }}</span
          ><span v-if="t.id === activeTournamentId" class="badge success">Active workspace</span
          ><span v-else class="badge">{{ t.abbrev }}</span>
        </div>
        <h3>{{ t.name }}</h3>
        <p>{{ t.start_date }} → {{ t.end_date }}</p>
        <div class="card-divider"></div>
        <button class="text-button" @click="open(t)">Manage tournament <span>→</span></button>
      </article>
    </div>
  </template>
</template>
