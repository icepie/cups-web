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
            <p class="text-sm text-muted mt-1">扫描后生成 PDF，仅在本次浏览器会话中保留。</p>
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

        <div v-else class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <UFormField label="扫描仪" class="sm:col-span-3">
            <USelect v-model="device" :items="scannerItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="色彩模式">
            <USelect v-model="mode" :items="modeItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="分辨率">
            <USelect v-model="resolution" :items="resolutionItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="输出格式">
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

    <UCard v-if="scanUrl">
      <template #header>
        <div class="flex items-center justify-between gap-3 flex-wrap">
          <h2 class="text-lg font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-file-text" class="w-5 h-5" />
            扫描结果
          </h2>
          <div class="flex gap-2">
            <UButton v-if="scanOutput === 'png'" icon="i-lucide-maximize-2" variant="outline" @click="showImagePreview">
              放大预览
            </UButton>
            <UButton :href="scanUrl" :download="scanFilename" icon="i-lucide-download" variant="outline">
              下载 {{ scanOutput.toUpperCase() }}
            </UButton>
          </div>
        </div>
      </template>
      <iframe v-if="scanOutput === 'pdf'" :src="scanUrl" title="扫描结果预览" class="w-full h-[70vh] rounded border border-default bg-white" />
      <div v-else class="flex justify-center rounded border border-default bg-muted/20 p-3">
        <img ref="scanImage" :src="scanUrl" alt="扫描结果" class="max-h-[70vh] cursor-zoom-in rounded object-contain" @load="initializeImageViewer">
      </div>
    </UCard>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import Viewer from 'viewerjs'
import 'viewerjs/dist/viewer.css'
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
const scanOutput = ref('')
const scanImage = ref(null)
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

const scannerItems = computed(() => scanners.value.map(scanner => ({
  label: scanner.name,
  value: scanner.id
})))

const scanProgressDescription = computed(() => `扫描仪正在传输图像，已用时 ${scanElapsedSeconds.value} 秒。设备未提供可靠的百分比时会在完成后显示结果。`)
const scanFilename = computed(() => `scan.${scanOutput.value}`)

let scanTimer

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

let imageViewer

function destroyImageViewer() {
  if (imageViewer) {
    imageViewer.destroy()
    imageViewer = undefined
  }
}

async function initializeImageViewer() {
  destroyImageViewer()
  await nextTick()
  if (scanImage.value) {
    imageViewer = new Viewer(scanImage.value, { navbar: false, title: false })
  }
}

function showImagePreview() {
  imageViewer?.show()
}

function clearScan() {
  destroyImageViewer()
  if (scanUrl.value) {
    URL.revokeObjectURL(scanUrl.value)
    scanUrl.value = ''
  }
  scanOutput.value = ''
}

async function loadScanners(refresh = false) {
  loading.value = true
  error.value = ''
  try {
    const resp = await apiFetch(`/api/scanners${refresh ? '?refresh=true' : ''}`)
    if (!resp.ok) {
      throw new Error(await readError(resp))
    }
    scanners.value = await resp.json()
    if (!scanners.value.some(scanner => scanner.id === device.value)) {
      device.value = scanners.value[0]?.id || ''
    }
  } catch (err) {
    scanners.value = []
    device.value = ''
    error.value = err.message || '读取扫描仪失败'
  } finally {
    loaded.value = true
    loading.value = false
  }
}

async function scan() {
  scanning.value = true
  startScanTimer()
  error.value = ''
  clearScan()
  try {
    const resp = await apiFetch('/api/scan', {
      method: 'POST',
      body: JSON.stringify({
        device: device.value,
        mode: mode.value,
        resolution: resolution.value,
        output: output.value
      })
    })
    if (!resp.ok) {
      throw new Error(await readError(resp))
    }
    const scanFile = await resp.blob()
    if (scanFile.size === 0) {
      throw new Error('扫描结果为空')
    }
    scanOutput.value = output.value
    scanUrl.value = URL.createObjectURL(scanFile)
  } catch (err) {
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
