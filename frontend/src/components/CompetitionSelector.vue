<script setup lang="ts">
import { computed } from 'vue'
import type { League, Stage } from '@/lib/types'
const props = defineProps<{
  leagues: League[]
  stages: Stage[]
  leagueId: number
  stageId: number
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:leagueId': [id: number]; selectStage: [stage: Stage] }>()
const mainLeagues = computed(() => props.leagues.filter((league) => !league.parent_id))
const selectedLeague = computed(() => props.leagues.find((league) => league.id === props.leagueId))
const visibleStages = computed(() =>
  props.leagueId
    ? props.stages
        .filter(
          (stage) =>
            stage.league_id === props.leagueId ||
            props.leagues.find((league) => league.id === stage.league_id)?.parent_id ===
              props.leagueId,
        )
        .sort((a, b) => a.league_id - b.league_id || a.sort_order - b.sort_order || a.id - b.id)
    : [],
)
const leagueName = (id: number) =>
  props.leagues.find((league) => league.id === id)?.name ?? 'League'
</script>
<template>
  <div
    class="grid grid-cols-[minmax(180px,0.9fr)_minmax(0,1.1fr)] items-start gap-[22px] max-[760px]:grid-cols-1"
  >
    <section
      class="overflow-hidden rounded-[10px] border border-[color:var(--border)] bg-white"
      aria-labelledby="competition-leagues-title"
    >
      <h2
        id="competition-leagues-title"
        class="m-0 bg-[#203b58] px-[18px] py-[14px] text-[15px] font-semibold text-white"
      >
        Leagues
      </h2>
      <nav aria-label="Choose league" class="max-h-[380px] overflow-y-auto">
        <div v-for="main in mainLeagues" :key="main.id">
          <button
            type="button"
            class="flex w-full items-center justify-between gap-[12px] border-b border-[#edf0f4] px-[18px] py-[14px] text-left text-[12px] font-semibold transition-colors hover:bg-[#f4f7ee] disabled:cursor-default"
            :class="leagueId === main.id ? 'bg-[#edf4df] text-[#3e6228]' : 'bg-[#fafbfd] text-ink'"
            :disabled="disabled"
            :aria-pressed="leagueId === main.id"
            @click="emit('update:leagueId', main.id)"
          >
            <span>{{ main.name }}</span
            ><span aria-hidden="true">›</span>
          </button>
          <button
            v-for="league in leagues.filter((item) => item.parent_id === main.id)"
            :key="league.id"
            type="button"
            class="flex w-full items-center justify-between gap-[12px] border-b border-[#edf0f4] py-[13px] pl-[32px] pr-[18px] text-left text-[12px] transition-colors hover:bg-[#f4f7ee] disabled:cursor-default"
            :class="
              leagueId === league.id
                ? 'bg-[#edf4df] font-semibold text-[#3e6228]'
                : 'bg-white text-[#596b80]'
            "
            :disabled="disabled"
            :aria-pressed="leagueId === league.id"
            @click="emit('update:leagueId', league.id)"
          >
            <span>{{ league.name }}</span
            ><span aria-hidden="true">›</span>
          </button>
        </div>
        <p v-if="!leagues.length" class="m-0 px-[18px] py-[24px] text-[12px] text-muted">
          No leagues configured.
        </p>
      </nav>
    </section>
    <section aria-labelledby="competition-stages-title">
      <h2 id="competition-stages-title" class="mb-[14px] text-[17px]">
        Stages<span v-if="selectedLeague" class="ml-[6px] text-[12px] font-normal text-muted"
          >· {{ selectedLeague.name }}</span
        >
      </h2>
      <p
        v-if="!selectedLeague"
        class="hint rounded-[9px] border border-dashed border-[#dce4ed] p-[22px]"
      >
        Choose a league to see its stages.
      </p>
      <p
        v-else-if="!visibleStages.length"
        class="hint rounded-[9px] border border-dashed border-[#dce4ed] p-[22px]"
      >
        No stages in this league yet. Add a stage in tournament setup.
      </p>
      <nav
        v-else
        aria-label="Choose stage"
        class="max-h-[380px] space-y-[10px] overflow-y-auto p-[2px]"
      >
        <button
          v-for="stage in visibleStages"
          :key="stage.id"
          type="button"
          class="flex w-full items-center justify-between gap-[14px] rounded-[7px] border px-[16px] py-[13px] text-left transition-colors hover:border-[#a8c472] hover:bg-[#f8fbf2] disabled:cursor-default"
          :class="
            stage.id === stageId
              ? 'border-[#bed491] bg-[#f2f7e9] text-[#3e6228]'
              : 'border-[#dfe6ee] bg-white text-[#3c526c]'
          "
          :disabled="disabled"
          :aria-pressed="stage.id === stageId"
          @click="emit('selectStage', stage)"
        >
          <span
            ><strong class="block text-[12px] font-medium">{{ stage.name }}</strong
            ><small class="mt-[5px] block text-[10px] text-muted"
              >{{ leagueName(stage.league_id) }} · {{ stage.type.replace('_', ' ') }}</small
            ></span
          ><span aria-hidden="true">→</span>
        </button>
      </nav>
    </section>
  </div>
</template>
