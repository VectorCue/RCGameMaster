<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { currentUser, logout } from '@/lib/auth'
import {
  activeTournament,
  activeTournamentId,
  canAdmin,
  canEdit,
  canReferee,
  errorMessage,
  loadTournaments,
  selectTournament,
  tournaments,
} from '@/lib/api'
const router = useRouter()
const error = ref('')
const menuOpen = ref(false)
onMounted(async () => {
  try {
    await loadTournaments()
  } catch (e) {
    error.value = errorMessage(e)
  }
})
function changeTournament(event: Event) {
  const id = Number((event.target as HTMLSelectElement).value)
  selectTournament(id)
  void router.push(`/tournaments/${id}/schedule`)
}
async function signOut() {
  try {
    await logout()
  } catch (e) {
    error.value = errorMessage(e)
  }
}
</script>
<template>
  <div class="app-layout flex min-h-screen">
    <aside class="sidebar" :class="{ open: menuOpen }">
      <RouterLink to="/" class="brand"
        ><span class="brand-mark">R<span>↗</span></span
        ><span>RCJV<span class="brand-sub">GAME MASTER</span></span></RouterLink
      >
      <div class="sidebar-label">WORKSPACE</div>
      <label class="tournament-select"
        ><span class="sr-only">Active tournament</span
        ><select :value="activeTournamentId ?? ''" @change="changeTournament">
          <option v-if="!tournaments.length" value="">No tournaments yet</option>
          <option v-for="t in tournaments" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select></label
      >
      <nav @click="menuOpen = false">
        <RouterLink to="/" class="nav-link"><span>◫</span> Overview</RouterLink>
        <RouterLink
          v-if="activeTournament"
          :to="`/tournaments/${activeTournament.id}/games`"
          class="nav-link"
          ><span>✣</span> Stage games</RouterLink
        >
        <RouterLink
          v-if="activeTournament"
          :to="`/tournaments/${activeTournament.id}/schedule`"
          class="nav-link"
          ><span>▦</span> Match schedule</RouterLink
        >
        <RouterLink
          v-if="activeTournament"
          :to="`/tournaments/${activeTournament.id}/standings`"
          class="nav-link"
          ><span>≡</span> Standings</RouterLink
        >
        <RouterLink
          v-if="activeTournament && canAdmin"
          :to="`/tournaments/${activeTournament.id}/setup`"
          class="nav-link"
          ><span>⚙</span> Tournament setup</RouterLink
        >
        <RouterLink
          v-if="activeTournament && canEdit"
          :to="`/tournaments/${activeTournament.id}/review`"
          class="nav-link"
          ><span>✓</span> Result review</RouterLink
        >
        <RouterLink v-if="canReferee" to="/referee" class="nav-link"
          ><span>⚑</span> Referee desk</RouterLink
        >
      </nav>
      <template v-if="activeTournament"
        ><div class="sidebar-label">GO LIVE</div>
        <nav>
          <RouterLink :to="`/public/${activeTournament.abbrev}`" class="nav-link"
            ><span>◎</span> Public tournament <small>↗</small></RouterLink
          ><RouterLink :to="`/display/${activeTournament.abbrev}`" class="nav-link"
            ><span>▣</span> Screen display <small>↗</small></RouterLink
          >
        </nav></template
      >
      <div class="sidebar-footer">
        <span class="avatar">{{
          (currentUser?.username || currentUser?.email || 'U').slice(0, 2).toUpperCase()
        }}</span>
        <div>
          <strong>{{ currentUser?.username || currentUser?.email || 'Signed in' }}</strong
          ><small>{{ currentUser?.roles?.join(' · ') || 'Viewer' }}</small>
        </div>
        <button class="icon-button" title="Sign out" aria-label="Sign out" @click="signOut">
          ↪
        </button>
      </div>
    </aside>
    <div class="app-content flex min-w-0 flex-col">
      <header class="topbar">
        <button
          class="icon-button mobile-menu"
          aria-label="Toggle menu"
          @click="menuOpen = !menuOpen"
        >
          ☰</button
        ><span class="breadcrumb"
          >Tournament operations <span>/</span> {{ activeTournament?.name || 'Overview' }}</span
        ><span class="status-dot">{{
          currentUser?.localDevelopment ? 'Local development' : 'Connected'
        }}</span>
      </header>
      <main class="content-main mx-auto min-w-0 w-full flex-1">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <slot />
      </main>
      <footer class="app-footer">
        RCJV Game Master <span>Built for the next generation of robotics.</span>
      </footer>
    </div>
  </div>
</template>
