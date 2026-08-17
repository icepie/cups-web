<template>
  <div class="p-3 sm:p-4 md:p-6 max-w-6xl mx-auto space-y-4">
    <UCard>
      <template #header>
        <div class="flex items-center justify-between gap-3 flex-wrap">
          <div>
            <h2 class="text-xl font-bold flex items-center gap-2">
              <UIcon name="i-lucide-scan-line" class="w-5 h-5" />
              扫描
            </h2>
            <p class="text-sm text-muted mt-1">扫描过程中实时显示图像，完成后可移动、旋转、裁剪并导出。</p>
          </div>
          <UButton
            variant="outline"
            icon="i-lucide-refresh-cw"
            :loading="loading"
            :disabled="scanning"
            @click="loadScanners(true)"
          >
            刷新设备
          </UButton>
        </div>
      </template>

      <div class="space-y-4">
        <UAlert v-if="error" color="error" variant="subtle" :title="error" />
        <UAlert
          v-if="scanning"
          color="info"
          variant="subtle"
          title="正在扫描"
          :description="scanProgressDescription"
        />
        <UAlert
          v-else-if="loaded && scanners.length === 0"
          color="warning"
          variant="subtle"
          title="未检测到扫描仪"
          description="请检查扫描仪的 USB 连接后刷新设备。"
        />

        <div v-else class="grid grid-cols-1 sm:grid-cols-4 gap-3">
          <UFormField label="扫描仪" class="sm:col-span-4">
            <USelect v-model="device" :items="scannerItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="色彩模式">
            <USelect v-model="mode" :items="modeItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="分辨率">
            <USelect v-model="resolution" :items="resolutionItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="默认导出格式">
            <USelect v-model="output" :items="outputItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <div class="flex items-end">
            <UButton
              class="w-full justify-center"
              icon="i-lucide-scan-line"
              :loading="scanning"
              :disabled="!device || scanning"
              @click="scan"
            >
              开始扫描
            </UButton>
          </div>
        </div>
      </div>
    </UCard>

    <UCard v-if="scanning || scanUrl">
      <template #header>
        <div class="flex items-center justify-between gap-3 flex-wrap">
          <h2 class="text-lg font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-file-image" class="w-5 h-5" />
            {{ scanning ? '实时扫描预览' : '扫描结果编辑' }}
          </h2>
          <div v-if="!scanning" class="flex gap-2 flex-wrap">
            <UButton size="sm" variant="outline" icon="i-lucide-move" @click="setEditorMode('move')">移动</UButton>
            <UButton size="sm" variant="outline" icon="i-lucide-crop" @click="setEditorMode('crop')">裁剪</UButton>
            <UButton size="sm" variant="outline" icon="i-lucide-rotate-ccw" @click="rotate(-90)">左旋</UButton>
            <UButton size="sm" variant="outline" icon="i-lucide-rotate-cw" @click="rotate(90)">右旋</UButton>
            <UButton size="sm" variant="ghost" icon="i-lucide-undo-2" @click="resetEditor">重置</UButton>
          </div>
        </div>
      </template>

      <div v-if="scanning" class="space-y-3">
        <canvas ref="liveCanvas" class="w-full rounded border border-default bg-white" />
        <UProgress :model-value="scanProgress" max="100" />
      </div>
      <div v-else class="space-y-3">
        <div class="relative max-h-[70vh] overflow-hidden rounded border border-default bg-elevated p-2">
          <img ref="editorImage" :src="scanUrl" alt="扫描结果" class="mx-auto block max-h-[68vh] max-w-full" />
        </div>
        <div class="flex items-center justify-end gap-2 flex-wrap">
          <UButton variant="outline" icon="i-lucide-check" @click="applyCrop">应用当前裁剪</UButton>
          <UButton color="primary" :icon="output === 'pdf' ? 'i-lucide-file-down' : 'i-lucide-image-down'" @click="exportSelected">
            导出 {{ output.toUpperCase() }}
          </UButton>
        </div>
      </div>
    </UCard>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import Cropper from 'cropperjs'
import 'cropperjs/dist/cropper.min.css'
import { apiFetch, readError } from '../utils/api'

const scanners = ref([])
const device = ref('')
const mode = ref('Color')
const resolution = ref(300)
const output = ref('pdf')
const loading = ref(false)
const loaded = ref(false)
const scanning = ref(false)
const error = ref('')
const scanUrl = ref('')
const liveCanvas = ref(null)
const editorImage = ref(null)
const scanRows = ref(0)
const scanHeight = ref(0)
const scanElapsedSeconds = ref(0)

const modeItems = [
  { label: '彩色', value: 'Color' },
  { label: '灰度', value: 'Gray' },
  { label: '黑白', value: 'Lineart' }
]
const resolutionItems = [75, 100, 150, 200, 300, 600].map(value => ({ label: `${value} DPI`, value }))
const outputItems = [
  { label: 'PDF 文档', value: 'pdf' },
  { label: 'PNG 图片', value: 'png' }
]
const scannerItems = computed(() => scanners.value.map(scanner => ({ label: scanner.name, value: scanner.id })))
const scanProgress = computed(() => scanHeight.value ? Math.round(scanRows.value / scanHeight.value * 100) : 0)
const scanProgressDescription = computed(() => {
  const rows = scanHeight.value ? `${scanRows.value}/${scanHeight.value} 行` : '正在读取扫描尺寸'
  return `${rows}，已用时 ${scanElapsedSeconds.value} 秒。图像会随扫描仪数据实时更新。`
})

let scanTimer
let cropper

function startScanTimer() {
  stopScanTimer()
  const startedAt = Date.now()
  scanElapsedSeconds.value = 0
  scanTimer = window.setInterval(() => {
    scanElapsedSeconds.value = Math.floor((Date.now() - startedAt) / 1000)
  }, 1000)
}

function stopScanTimer() {
  if (scanTimer) {
    window.clearInterval(scanTimer)
    scanTimer = undefined
  }
}

function destroyCropper() {
  if (cropper) {
    cropper.destroy()
    cropper = undefined
  }
}

async function initializeCropper() {
  destroyCropper()
  await nextTick()
  if (!editorImage.value) return
  cropper = new Cropper(editorImage.value, {
    viewMode: 1,
    dragMode: 'move',
    autoCrop: false,
    responsive: true,
    background: true,
    ready() {
      cropper.setData({ x: 0, y: 0, width: editorImage.value.naturalWidth, height: editorImage.value.naturalHeight })
    }
  })
}

function setEditorMode(modeName) {
  if (!cropper) return
  cropper.setDragMode(modeName)
  if (modeName === 'crop') cropper.crop()
}

function rotate(degrees) {
  cropper?.rotate(degrees)
}

function resetEditor() {
  cropper?.reset()
  cropper?.setDragMode('move')
}

function canvasToBlob(canvas, type = 'image/png') {
  return new Promise((resolve, reject) => {
    canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('无法生成导出文件')), type)
  })
}

function getEditedCanvas() {
  if (!cropper) throw new Error('扫描结果尚未准备好')
  return cropper.getCroppedCanvas({
    maxWidth: 6000,
    maxHeight: 6000,
    fillColor: '#fff',
    imageSmoothingEnabled: true,
    imageSmoothingQuality: 'high'
  })
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

async function exportPNG() {
  try {
    downloadBlob(await canvasToBlob(getEditedCanvas()), 'scan.png')
  } catch (err) {
    error.value = err.message || '导出 PNG 失败'
  }
}

async function exportPDF() {
  try {
    const form = new FormData()
    form.append('file', await canvasToBlob(getEditedCanvas()), 'scan.png')
    form.append('orientation', 'portrait')
    form.append('paper_size', 'A4')
    form.append('name', 'scan')
    const resp = await apiFetch('/api/convert', { method: 'POST', body: form })
    if (!resp.ok) throw new Error(await readError(resp))
    downloadBlob(await resp.blob(), 'scan.pdf')
  } catch (err) {
    error.value = err.message || '导出 PDF 失败'
  }
}

function exportSelected() {
	return output.value === 'pdf' ? exportPDF() : exportPNG()
}

async function applyCrop() {
  try {
    const nextUrl = URL.createObjectURL(await canvasToBlob(getEditedCanvas()))
    const previousUrl = scanUrl.value
    scanUrl.value = nextUrl
    URL.revokeObjectURL(previousUrl)
    await initializeCropper()
  } catch (err) {
    error.value = err.message || '应用裁剪失败'
  }
}

function clearScan() {
  destroyCropper()
  if (scanUrl.value) URL.revokeObjectURL(scanUrl.value)
  scanUrl.value = ''
  scanRows.value = 0
  scanHeight.value = 0
}

async function loadScanners(refresh = false) {
  loading.value = true
  error.value = ''
  try {
    const resp = await apiFetch(`/api/scanners${refresh ? '?refresh=true' : ''}`)
    if (!resp.ok) throw new Error(await readError(resp))
    scanners.value = await resp.json()
    if (!scanners.value.some(scanner => scanner.id === device.value)) device.value = scanners.value[0]?.id || ''
  } catch (err) {
    scanners.value = []
    device.value = ''
    error.value = err.message || '读取扫描仪失败'
  } finally {
    loaded.value = true
    loading.value = false
  }
}

async function readScanStream(response) {
  const reader = response.body?.getReader()
  if (!reader) throw new Error('浏览器不支持扫描流')
  let pending = new Uint8Array(0)
  let offset = 0
  const decoder = new TextDecoder()
  async function pull() {
    const { value, done } = await reader.read()
    if (done) throw new Error('扫描数据不完整')
    const rest = pending.subarray(offset)
    const merged = new Uint8Array(rest.length + value.length)
    merged.set(rest)
    merged.set(value, rest.length)
    pending = merged
    offset = 0
  }
  async function line() {
    while (true) {
      const end = pending.indexOf(10, offset)
      if (end >= 0) {
        const result = decoder.decode(pending.subarray(offset, end))
        offset = end + 1
        return result
      }
      await pull()
    }
  }
  async function bytes(length) {
    while (pending.length - offset < length) await pull()
    const result = pending.subarray(offset, offset + length)
    offset += length
    return result
  }

  const metadata = JSON.parse(await line())
  const canvas = liveCanvas.value
  canvas.width = metadata.width
  canvas.height = metadata.height
  const context = canvas.getContext('2d', { alpha: false })
  const rowImage = context.createImageData(metadata.width, 1)
  scanHeight.value = metadata.height
  scanRows.value = 0
  for (let y = 0; y < metadata.height; y++) {
    const raw = await bytes(metadata.rowBytes)
    const pixels = rowImage.data
    if (metadata.magic === 'P6') {
      for (let x = 0; x < metadata.width; x++) {
        const source = x * 3
        const target = x * 4
        pixels[target] = raw[source]
        pixels[target + 1] = raw[source + 1]
        pixels[target + 2] = raw[source + 2]
        pixels[target + 3] = 255
      }
    } else if (metadata.magic === 'P5') {
      for (let x = 0; x < metadata.width; x++) {
        const value = raw[x]
        const target = x * 4
        pixels[target] = value
        pixels[target + 1] = value
        pixels[target + 2] = value
        pixels[target + 3] = 255
      }
    } else {
      for (let x = 0; x < metadata.width; x++) {
        const value = (raw[Math.floor(x / 8)] & (0x80 >> (x % 8))) ? 0 : 255
        const target = x * 4
        pixels[target] = value
        pixels[target + 1] = value
        pixels[target + 2] = value
        pixels[target + 3] = 255
      }
    }
    context.putImageData(rowImage, 0, y)
    scanRows.value = y + 1
    if (y % 8 === 0) await new Promise(resolve => requestAnimationFrame(resolve))
  }
  const blob = await canvasToBlob(canvas)
  scanUrl.value = URL.createObjectURL(blob)
  await initializeCropper()
}

async function scan() {
  scanning.value = true
  startScanTimer()
  error.value = ''
  clearScan()
  try {
    const resp = await apiFetch('/api/scan/stream', {
      method: 'POST',
      body: JSON.stringify({ device: device.value, mode: mode.value, resolution: resolution.value, output: output.value })
    })
    if (!resp.ok) throw new Error(await readError(resp))
    await readScanStream(resp)
  } catch (err) {
    clearScan()
    error.value = err.message || '扫描失败'
  } finally {
    stopScanTimer()
    scanning.value = false
  }
}

onMounted(loadScanners)
onUnmounted(() => {
  stopScanTimer()
  clearScan()
})
</script>
