<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { League } from '@/lib/types'

const props = defineProps<{ leagues: League[] }>()
const emit = defineEmits<{ select: [id: number] }>()
const roots = computed(() => props.leagues.filter((league) => !league.parent_id))
const expanded = ref<number | null>(null)
const main = computed(
  () => roots.value.find((league) => league.id === expanded.value) || roots.value[0],
)
const children = computed(() =>
  props.leagues.filter((league) => league.parent_id === main.value?.id),
)
watch(
  roots,
  (roots) => {
    if (!roots.some((league) => league.id === expanded.value)) expanded.value = roots[0]?.id || null
  },
  { immediate: true },
)
</script>

<template>
  <section class="league-chooser" aria-labelledby="choose-league-title">
    <header>
      <p>FOLLOW YOUR COMPETITION</p>
      <h2 id="choose-league-title">Choose a league</h2>
      <span>Select a main league, then open all its games or choose a sub-league.</span>
    </header>
    <div v-if="!roots.length" class="chooser-empty">No leagues are available yet.</div>
    <div v-else class="chooser-columns">
      <nav class="main-list" aria-label="Main leagues">
        <h3>Main leagues</h3>
        <button
          v-for="league in roots"
          :key="league.id"
          :class="{ active: main?.id === league.id }"
          :aria-expanded="main?.id === league.id"
          @click="expanded = league.id"
        >
          <strong>{{ league.name }}</strong
          ><span>→</span>
        </button>
      </nav>
      <div v-if="main" class="sub-list">
        <h3>{{ main.name }}</h3>
        <button class="open-main" @click="emit('select', main.id)">
          <span
            ><strong>{{
              children.length ? `All ${main.name} leagues` : `Open ${main.name}`
            }}</strong
            ><small>{{
              children.length
                ? 'Games, teams and published standings across this main league'
                : 'Games, teams and published standings'
            }}</small></span
          ><b>↗</b></button
        ><template v-if="children.length"
          ><p class="sub-heading">Or choose a sub-league</p>
          <button
            v-for="league in children"
            :key="league.id"
            class="open-child"
            @click="emit('select', league.id)"
          >
            <strong>{{ league.name }}</strong
            ><span>→</span>
          </button></template
        >
      </div>
    </div>
  </section>
</template>

<style scoped>
.league-chooser {
  background: #fff;
  border: 1px solid #dce6ec;
  border-radius: 14px;
  padding: 32px;
  margin: 8px 0 30px;
}
.league-chooser > header {
  margin-bottom: 28px;
}
.league-chooser > header > p {
  margin: 0 0 9px;
  color: #0a9680;
  font-size: 10px;
  letter-spacing: 0.16em;
  font-weight: 800;
}
.league-chooser h2 {
  margin: 0 0 10px;
  color: #21475d;
  font-size: 25px;
  letter-spacing: -0.5px;
}
.league-chooser > header > span {
  color: #7e96a5;
  font-size: 13px;
  line-height: 1.7;
}
.chooser-columns {
  display: grid;
  grid-template-columns: minmax(200px, 0.8fr) minmax(260px, 1.4fr);
  gap: 30px;
  align-items: start;
  max-width: 850px;
}
.main-list {
  border: 1px solid #ccdfe6;
  border-radius: 10px;
  overflow: hidden;
}
.main-list h3 {
  background: #244c63;
  color: #e7f2f5;
  font-size: 12px;
  margin: 0;
  padding: 15px 18px;
  font-weight: 650;
}
.main-list button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  width: 100%;
  padding: 17px 18px;
  border: 0;
  border-top: 1px solid #e4edf1;
  background: #fff;
  text-align: left;
  cursor: pointer;
  color: #668394;
  font-size: 13px;
}
.main-list button.active {
  color: #05836f;
  background: #eaf7f1;
  box-shadow: inset 3px 0 #059d83;
}
.main-list strong {
  font-weight: 650;
}
.main-list button > span {
  color: #9eb3bd;
}
.main-list button.active > span {
  color: #0b957e;
}
.sub-list h3 {
  margin: 0 0 14px;
  color: #36586d;
  font-size: 17px;
  font-weight: 650;
}
.open-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: 18px;
  text-align: left;
  padding: 18px;
  border: 1px solid #acd8cc;
  background: #f0fbf6;
  border-radius: 9px;
  color: #09816c;
  cursor: pointer;
}
.open-main > span {
  display: grid;
  gap: 8px;
}
.open-main strong {
  font-size: 14px;
  font-weight: 700;
}
.open-main small {
  font-size: 11px;
  font-weight: 400;
  color: #6f9b8f;
  line-height: 1.6;
}
.open-main > b {
  font-size: 22px;
  font-weight: 500;
}
.sub-heading {
  margin: 22px 0 12px;
  font-size: 11px;
  color: #92a7b3;
}
.open-child {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  width: 100%;
  padding: 16px 18px;
  border: 1px solid #d6e3e9;
  border-radius: 8px;
  margin: 8px 0;
  background: #fff;
  text-align: left;
  color: #537487;
  font-size: 13px;
  cursor: pointer;
}
.open-child strong {
  font-weight: 600;
}
.open-child span {
  color: #8eacb9;
}
.open-child:hover,
.main-list button:hover {
  background: #f3f9f8;
}
.open-main:hover {
  border-color: #119e81;
}
.chooser-empty {
  padding: 35px 16px;
  text-align: center;
  color: #91a5b3;
  font-size: 13px;
}
button:focus-visible {
  outline: 3px solid #1ba58b55;
  outline-offset: 3px;
}
@media (max-width: 600px) {
  .league-chooser {
    padding: 22px;
  }
  .chooser-columns {
    grid-template-columns: 1fr;
    gap: 24px;
  }
  .league-chooser h2 {
    font-size: 22px;
  }
  .league-chooser > header > span {
    font-size: 12px;
  }
  .main-list button {
    padding: 14px 16px;
  }
  .open-child {
    padding: 15px 16px;
  }
  .sub-list h3 {
    font-size: 16px;
  }
}
</style>
