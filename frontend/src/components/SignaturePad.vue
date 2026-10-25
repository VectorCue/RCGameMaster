<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{ modelValue: string; label: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const canvas = ref<HTMLCanvasElement>()
let drawing = false
let observer: ResizeObserver | undefined

function context() {
  return canvas.value?.getContext('2d')
}
function redraw() {
  const el = canvas.value
  if (!el) return
  const bounds = el.getBoundingClientRect()
  const ratio = window.devicePixelRatio || 1
  el.width = Math.max(1, Math.floor(bounds.width * ratio))
  el.height = Math.floor(160 * ratio)
  const ctx = context()
  if (!ctx) return
  ctx.scale(ratio, ratio)
  ctx.lineWidth = 2.4
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.strokeStyle = '#142d43'
  if (props.modelValue) {
    const picture = new Image()
    picture.onload = () => ctx.drawImage(picture, 0, 0, bounds.width, 160)
    picture.src = props.modelValue
  }
}
function position(event: PointerEvent) {
  const bounds = canvas.value!.getBoundingClientRect()
  return { x: event.clientX - bounds.left, y: event.clientY - bounds.top }
}
function begin(event: PointerEvent) {
  if (event.button !== 0) return
  const el = canvas.value
  const ctx = context()
  if (!el || !ctx) return
  event.preventDefault()
  el.setPointerCapture(event.pointerId)
  drawing = true
  const p = position(event)
  ctx.beginPath()
  ctx.moveTo(p.x, p.y)
  // A visible dot also records a deliberate tap.
  ctx.lineTo(p.x + 0.1, p.y + 0.1)
  ctx.stroke()
}
function move(event: PointerEvent) {
  if (!drawing) return
  event.preventDefault()
  const p = position(event)
  context()?.lineTo(p.x, p.y)
  context()?.stroke()
}
function finish() {
  if (!drawing) return
  drawing = false
  emit('update:modelValue', canvas.value!.toDataURL('image/png'))
}
function clear() {
  drawing = false
  const el = canvas.value
  if (el) context()?.clearRect(0, 0, el.width, el.height)
  emit('update:modelValue', '')
}
watch(
  () => props.modelValue,
  (value) => {
    if (!value) redraw()
  },
)
onMounted(() => {
  redraw()
  observer = new ResizeObserver(redraw)
  if (canvas.value) observer.observe(canvas.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div class="signature-pad">
    <div class="signature-heading">
      <span>{{ label }}</span
      ><button class="signature-clear" type="button" @click="clear">Clear</button>
    </div>
    <canvas
      ref="canvas"
      :aria-label="`Draw signature for ${label}`"
      @pointerdown="begin"
      @pointermove="move"
      @pointerup="finish"
      @pointercancel="finish"
      @lostpointercapture="finish"
    />
    <p>{{ modelValue ? 'Signature recorded' : 'Sign above with a finger, stylus, or mouse.' }}</p>
  </div>
</template>

<style scoped>
.signature-pad {
  border: 1px solid #ccd8e1;
  border-radius: 12px;
  overflow: hidden;
  background: #fff;
}
.signature-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  font-weight: 700;
  color: #203e52;
  border-bottom: 1px solid #e3eaf0;
}
.signature-clear {
  background: #edf3f7;
  color: #315269;
  border: 0;
  border-radius: 6px;
  padding: 6px 12px;
  cursor: pointer;
}
canvas {
  width: 100%;
  height: 160px;
  display: block;
  touch-action: none;
  background: linear-gradient(transparent 124px, #dae2e9 125px, transparent 126px);
}
p {
  padding: 8px 16px;
  margin: 0;
  font-size: 12px;
  color: #647789;
  background: #f8fafc;
}
</style>
