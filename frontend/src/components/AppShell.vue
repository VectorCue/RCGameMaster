<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const collapsed = ref(false)
const drawer = ref<HTMLDialogElement>()
const drawerOpen = ref(false)
</script>

<template>
</template>
