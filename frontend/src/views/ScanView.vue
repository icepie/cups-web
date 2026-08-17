<template>
  <div class="w-full max-w-none p-3 sm:p-4 md:p-6 xl:px-8 space-y-4">
    <UCard>
      <div class="flex items-center gap-2 flex-wrap">
        <UButton variant="outline" icon="i-lucide-folder-plus" :disabled="scanning" @click="openNewProject">新建项目</UButton>
        <UButton variant="outline" icon="i-lucide-history" :disabled="scanning" @click="openProjectHistory">历史项目</UButton>
        <span v-if="activeProject" class="max-w-48 truncate text-sm font-medium" :title="activeProject.name">{{ activeProject.name }}</span>

        <div class="mx-1 h-6 border-l border-default hidden sm:block" />

        <UButton icon="i-lucide-scan-line" :loading="scanning" :disabled="!device || !activeProject || scanning" @click="scan">
          {{ pages.length ? '扫描下一页' : '开始扫描' }}
        </UButton>
        <UButton v-if="scanning" color="error" variant="outline" icon="i-lucide-square" @click="cancelScan">停止</UButton>
        <UButton variant="outline" icon="i-lucide-refresh-cw" :loading="loading" :disabled="scanning" @click="loadScanners(true)">刷新设备</UButton>

        <div class="mx-1 h-6 border-l border-default hidden sm:block" />

        <UButton variant="ghost" icon="i-lucide-rotate-ccw" :disabled="!currentPage || scanning" @click="rotate(-90)">左旋</UButton>
        <UButton variant="ghost" icon="i-lucide-rotate-cw" :disabled="!currentPage || scanning" @click="rotate(90)">右旋</UButton>
        <UButton variant="ghost" icon="i-lucide-crop" :disabled="!currentPage || scanning" @click="setEditorMode('crop')">裁剪</UButton>
        <UButton variant="ghost" icon="i-lucide-trash-2" :disabled="!currentPage || scanning" @click="deleteSelectedPage">删除</UButton>

        <div class="ml-auto flex items-center gap-2 flex-wrap">
          <UButton variant="outline" icon="i-lucide-file-down" :disabled="pages.length === 0 || scanning" @click="exportPDF">保存 PDF</UButton>
          <UButton variant="outline" icon="i-lucide-image-down" :disabled="pages.length === 0 || scanning" @click="exportImages">导出图片</UButton>
        </div>
      </div>
    </UCard>

    <UAlert v-if="error" color="error" variant="subtle" :title="error" />
    <UAlert v-if="!activeProject" color="info" variant="subtle" title="请选择或新建扫描项目" description="扫描的页面会自动保存在当前项目，可随时从历史项目载入。" />
    <UAlert v-if="scanning" color="info" variant="subtle" title="正在扫描" :description="scanProgressDescription" />
    <UAlert v-else-if="loaded && scanners.length === 0" color="warning" variant="subtle" title="未检测到扫描仪" description="请检查扫描仪的 USB 连接后刷新设备。" />

    <div class="grid grid-cols-1 lg:grid-cols-[17rem_minmax(0,1fr)] gap-4">
      <UCard>
        <template #header>
          <div class="flex items-center justify-between gap-2">
            <span class="font-semibold">扫描设置</span>
            <span class="text-xs text-muted">{{ pages.length }} 页</span>
          </div>
        </template>

        <div class="space-y-4">
          <UFormField label="扫描仪">
            <USelect v-model="device" :items="scannerItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="文档类型">
            <div class="grid grid-cols-2 gap-2">
              <UButton :variant="scanPreset === 'text' ? 'solid' : 'outline'" size="sm" icon="i-lucide-file-text" @click="applyPreset('text')">文本</UButton>
              <UButton :variant="scanPreset === 'photo' ? 'solid' : 'outline'" size="sm" icon="i-lucide-image" @click="applyPreset('photo')">图片</UButton>
            </div>
          </UFormField>
          <UFormField label="色彩模式">
            <USelect v-model="mode" :items="modeItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <UFormField label="分辨率">
            <USelect v-model="resolution" :items="resolutionItems" value-key="value" label-key="label" class="w-full" />
          </UFormField>
          <div class="border-t border-default pt-4">
            <p class="mb-3 text-xs font-semibold text-muted">导出图片</p>
            <div class="space-y-4">
              <UFormField label="图片格式">
                <USelect v-model="imageFormat" :items="imageFormatItems" value-key="value" label-key="label" class="w-full" />
              </UFormField>
              <UFormField label="图片质量" :hint="imageFormat === 'png' ? 'PNG 为无损格式，质量不生效' : `${Math.round(imageQuality * 100)}%`">
                <input v-model.number="imageQuality" class="w-full accent-primary disabled:opacity-50" type="range" min="0.5" max="1" step="0.05" :disabled="imageFormat === 'png'">
              </UFormField>
            </div>
          </div>
          <p class="text-xs text-muted leading-5">每次扫描会追加到文档末尾。PDF 保留全部页面；格式与质量仅作用于「导出图片」。</p>
        </div>
      </UCard>

      <UCard class="min-w-0">
        <template #header>
          <div class="flex items-center justify-between gap-3 flex-wrap">
            <div>
              <h2 class="text-lg font-semibold">{{ scanning ? '正在扫描第 ' + nextPageNumber + ' 页' : '文档预览' }}</h2>
              <p class="text-sm text-muted">选择页面后可拖动平移、滚轮缩放、裁剪、删除或调整顺序。</p>
            </div>
            <div v-if="currentPage && !scanning" class="flex items-center gap-2 flex-wrap">
              <UButton size="sm" variant="outline" icon="i-lucide-move" @click="setEditorMode('move')">移动</UButton>
              <UButton size="sm" variant="outline" icon="i-lucide-check" @click="applyCrop">应用裁剪</UButton>
              <div class="flex items-center gap-1 rounded border border-default px-1 py-0.5" title="也可用鼠标滚轮缩放，拖动画面平移">
                <UButton size="xs" variant="ghost" icon="i-lucide-zoom-out" aria-label="缩小" @click="zoomBy(-0.1)" />
                <input :value="zoomPercent" class="w-16 accent-primary" type="range" min="10" max="500" step="5" aria-label="缩放比例" @input="setZoom(Number($event.target.value))">
                <span class="w-10 text-center text-xs tabular-nums">{{ zoomPercent }}%</span>
                <UButton size="xs" variant="ghost" icon="i-lucide-zoom-in" aria-label="放大" @click="zoomBy(0.1)" />
                <UButton size="xs" variant="ghost" icon="i-lucide-maximize" aria-label="适应预览" @click="resetViewport" />
              </div>
              <UButton size="sm" variant="ghost" icon="i-lucide-undo-2" @click="resetSelectedPage">还原页面</UButton>
              <UButton size="sm" variant="ghost" icon="i-lucide-chevron-left" :disabled="selectedPageIndex === 0" @click="moveSelectedPage(-1)">前移</UButton>
              <UButton size="sm" variant="ghost" icon="i-lucide-chevron-right" :disabled="selectedPageIndex === pages.length - 1" @click="moveSelectedPage(1)">后移</UButton>
            </div>
          </div>
        </template>

        <div class="grid grid-cols-1 xl:grid-cols-[10rem_minmax(0,1fr)] gap-4 min-h-[32rem]">
          <aside class="flex xl:flex-col gap-2 overflow-x-auto xl:overflow-y-auto xl:max-h-[68vh] p-1">
            <button
              v-for="(page, index) in pages"
              :key="page.id"
              class="shrink-0 w-32 rounded border p-1 text-left transition-colors"
              :class="page.id === selectedPageId ? 'border-primary ring-1 ring-primary' : 'border-default hover:border-primary'"
              type="button"
              @click="selectPage(page.id)"
            >
              <img :src="page.url" :alt="`第 ${index + 1} 页`" class="h-36 w-full object-contain bg-white rounded">
              <span class="block px-1 pt-1 text-xs font-medium">第 {{ index + 1 }} 页</span>
            </button>
            <div v-if="scanning" class="shrink-0 w-32 rounded border border-primary bg-primary/5 p-1">
              <canvas ref="liveThumbnailCanvas" class="h-36 w-full object-contain bg-white rounded" />
              <span class="block px-1 pt-1 text-xs font-medium">扫描中…</span>
            </div>
          </aside>

          <div class="relative flex min-h-[30rem] items-center justify-center overflow-hidden rounded border border-default bg-elevated p-3">
            <template v-if="scanning">
              <canvas ref="liveCanvas" class="max-h-[64vh] max-w-full bg-white shadow-sm" />
              <UProgress class="absolute inset-x-5 bottom-4" :model-value="scanProgress" max="100" />
            </template>
            <img v-else-if="currentPage" ref="editorImage" :key="selectedPageId" :src="currentPage.url" alt="当前扫描页面" class="max-h-[64vh] max-w-full block">
            <div v-else class="text-center text-muted">
              <UIcon name="i-lucide-files" class="mx-auto mb-3 size-10" />
              <p>扫描页面将按顺序放在这里。</p>
            </div>
          </div>
        </div>
      </UCard>
    </div>
    <UModal v-model:open="showNewProject">
      <template #content>
        <UCard class="w-full">
          <template #header><h2 class="text-lg font-semibold">新建扫描项目</h2></template>
          <UFormField label="项目名称">
            <UInput v-model="newProjectName" autofocus maxlength="100" placeholder="例如：2026 年 8 月报销单" class="w-full" @keydown.enter="createProject" />
          </UFormField>
          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton variant="ghost" @click="showNewProject = false">取消</UButton>
              <UButton :loading="projectSaving" :disabled="!newProjectName.trim()" @click="createProject">创建并载入</UButton>
            </div>
          </template>
        </UCard>
      </template>
    </UModal>

    <UModal v-model:open="showProjectHistory">
      <template #content>
        <UCard class="w-full">
          <template #header>
            <div class="flex items-center justify-between gap-2">
              <h2 class="text-lg font-semibold">扫描项目历史</h2>
              <UButton size="sm" variant="ghost" icon="i-lucide-refresh-cw" :loading="projectLoading" @click="loadProjects">刷新</UButton>
            </div>
          </template>
          <div v-if="projects.length" class="max-h-[60vh] space-y-2 overflow-y-auto">
            <div v-for="project in projects" :key="project.id" class="flex items-center gap-3 rounded border border-default p-3">
              <UIcon name="i-lucide-folder" class="size-5 shrink-0 text-primary" />
              <button class="min-w-0 flex-1 text-left" type="button" @click="loadProject(project.id)">
                <p class="truncate font-medium">{{ project.name }}</p>
                <p class="text-xs text-muted">{{ project.pageCount }} 页 · {{ formatProjectTime(project.updatedAt) }}</p>
              </button>
              <UButton size="sm" variant="outline" @click="loadProject(project.id)">载入</UButton>
              <UButton size="sm" color="error" variant="ghost" icon="i-lucide-trash-2" :disabled="projectSaving" :aria-label="`删除 ${project.name}`" @click="deleteProject(project)" />
            </div>
          </div>
          <p v-else class="py-8 text-center text-sm text-muted">还没有保存的扫描项目。</p>
        </UCard>
      </template>
    </UModal>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import Cropper from 'cropperjs'
import 'cropperjs/dist/cropper.min.css'
import { apiFetch, readError } from '../utils/api'
import { PDFDocument } from 'pdf-lib'

const scanners = ref([])
const device = ref('')
const mode = ref('Color')
const resolution = ref(300)
const scanPreset = ref('photo')
const imageFormat = ref('png')
const imageQuality = ref(0.9)
const loading = ref(false)
const loaded = ref(false)
const scanning = ref(false)
const error = ref('')
const projects = ref([])
const activeProject = ref(null)
const showNewProject = ref(false)
const showProjectHistory = ref(false)
const newProjectName = ref('')
const projectLoading = ref(false)
const projectSaving = ref(false)
const pages = ref([])
const selectedPageId = ref('')
const liveCanvas = ref(null)
const liveThumbnailCanvas = ref(null)
const editorImage = ref(null)
const scanRows = ref(0)
const scanHeight = ref(0)
const scanElapsedSeconds = ref(0)

const zoomPercent = ref(100)
const modeItems = [
  { label: '彩色', value: 'Color' },
  { label: '灰度', value: 'Gray' },
  { label: '黑白', value: 'Lineart' }
]
const resolutionItems = [75, 100, 150, 200, 300, 600].map(value => ({ label: `${value} DPI`, value }))
const imageFormatItems = [
  { label: 'PNG（无损）', value: 'png' },
  { label: 'JPEG（较小）', value: 'jpeg' }
]
const scannerItems = computed(() => scanners.value.map(scanner => ({ label: scanner.name, value: scanner.id })))
const currentPage = computed(() => pages.value.find(page => page.id === selectedPageId.value))
const selectedPageIndex = computed(() => pages.value.findIndex(page => page.id === selectedPageId.value))
const nextPageNumber = computed(() => pages.value.length + 1)
const scanProgress = computed(() => scanHeight.value ? Math.round(scanRows.value / scanHeight.value * 100) : 0)
const scanProgressDescription = computed(() => {
  const rows = scanHeight.value ? `${scanRows.value}/${scanHeight.value} 行` : '正在读取扫描尺寸'
  return `${rows}，已用时 ${scanElapsedSeconds.value} 秒。`
})

let cropper
let scanTimer
let scanAbortController


function startScanTimer() {
  stopScanTimer()
  const startedAt = Date.now()
  scanElapsedSeconds.value = 0
  scanTimer = window.setInterval(() => {
    scanElapsedSeconds.value = Math.floor((Date.now() - startedAt) / 1000)
  }, 1000)
}

function stopScanTimer() {
  if (scanTimer) window.clearInterval(scanTimer)
  scanTimer = undefined
}

function destroyCropper() {
  cropper?.destroy()
  cropper = undefined
}

async function initializeCropper() {
  destroyCropper()
  await nextTick()
  if (!editorImage.value || !currentPage.value) return
  cropper = new Cropper(editorImage.value, {
    viewMode: 1,
    dragMode: 'move',
    autoCrop: false,
    responsive: true,
    background: true,
    zoomOnWheel: true,
    zoomOnTouch: true,
    zoom(event) {
      const ratio = event.detail.ratio
      if (ratio < 0.1 || ratio > 5) {
        event.preventDefault()
        zoomPercent.value = ratio < 0.1 ? 10 : 500
        return
      }
      zoomPercent.value = Math.round(ratio * 100)
    },
    ready() {
      cropper.setData({ x: 0, y: 0, width: editorImage.value.naturalWidth, height: editorImage.value.naturalHeight })
      zoomPercent.value = Math.round(cropper.getImageData().ratio * 100)
    }
  })
}

function applyPreset(preset) {
  scanPreset.value = preset
  if (preset === 'text') {
    mode.value = 'Gray'
    resolution.value = 600
  } else {
    mode.value = 'Color'
    resolution.value = 300
  }
}

function setEditorMode(modeName) {
  if (!cropper) return
  cropper.setDragMode(modeName)
  if (modeName === 'crop') cropper.crop()
}

function zoomBy(delta) {
  cropper?.zoom(delta)
}

function setZoom(percent) {
  cropper?.zoomTo(percent / 100)
}

function resetViewport() {
  cropper?.reset()
  zoomPercent.value = cropper ? Math.round(cropper.getImageData().ratio * 100) : 100
  setEditorMode('move')
}

function canvasToBlob(canvas, type = 'image/png', quality) {
  return new Promise((resolve, reject) => {
    canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('无法生成页面图像')), type, quality)
  })
}

function getEditedCanvas() {
  if (!cropper) throw new Error('当前页面尚未准备好')
  return cropper.getCroppedCanvas({
    maxWidth: 6000,
    maxHeight: 6000,
    fillColor: '#fff',
    imageSmoothingEnabled: true,
    imageSmoothingQuality: 'high'
  })
}

// 是否存在尚未落盘的裁剪：裁剪框已激活且与整幅画面不一致。
// 未裁剪时 cropper.getCropBoxData() 返回空对象，据此跳过无谓的重编码与上传。
function hasPendingCrop() {
  if (!cropper) return false
  const box = cropper.getCropBoxData()
  if (!box.width) return false
  const canvas = cropper.getCanvasData()
  const eps = 1
  return (
    Math.abs(box.left - canvas.left) > eps ||
    Math.abs(box.top - canvas.top) > eps ||
    Math.abs(box.width - canvas.width) > eps ||
    Math.abs(box.height - canvas.height) > eps
  )
}

function replacePageBlob(page, blob) {
  const previousUrl = page.url
  page.blob = blob
  page.url = URL.createObjectURL(blob)
  URL.revokeObjectURL(previousUrl)
}
async function commitCurrentEditor(force = false) {
  const page = currentPage.value
  if (!page || !cropper) return
  // 旋转会即时提交；此处只需处理未应用的裁剪框。无实际编辑时跳过，
  // 避免每次切换项目 / 导出都整页重编码并重新上传。
  if (!force && !hasPendingCrop()) return
  const previousBlob = page.blob
  replacePageBlob(page, await canvasToBlob(getEditedCanvas()))
  try {
    await persistEditedPage(page)
  } catch (err) {
    replacePageBlob(page, previousBlob)
    await initializeCropper()
    throw err
  }
  await initializeCropper()
}

async function rotate(degrees) {
  try {
    cropper?.rotate(degrees)
    await commitCurrentEditor(true)
  } catch (err) {
    error.value = err.message || '旋转页面失败'
  }
}

async function applyCrop() {
  try {
    await commitCurrentEditor()
    setEditorMode('move')
  } catch (err) {
    error.value = err.message || '应用裁剪失败'
  }
}

async function resetSelectedPage() {
  const page = currentPage.value
  if (!page || !activeProject.value) return
  try {
    if (!page.originalBlob) page.originalBlob = await fetchProjectImage(page.originalUrl)
    const response = await apiFetch(`/api/scan-projects/${activeProject.value.id}/pages/${page.id}/reset`, { method: 'DELETE' })
    if (!response.ok) throw new Error(await readError(response))
    replacePageBlob(page, page.originalBlob)
    await initializeCropper()
  } catch (err) {
    error.value = err.message || '还原页面失败'
  }
}

async function selectPage(id) {
  if (id === selectedPageId.value) return
  selectedPageId.value = id
  await initializeCropper()
}

async function moveSelectedPage(direction) {
  const index = selectedPageIndex.value
  const target = index + direction
  if (index < 0 || target < 0 || target >= pages.value.length || !activeProject.value) return
  const [page] = pages.value.splice(index, 1)
  pages.value.splice(target, 0, page)
  try {
    await savePageOrder()
    await initializeCropper()
  } catch (err) {
    pages.value.splice(target, 1)
    pages.value.splice(index, 0, page)
    error.value = err.message || '调整页面顺序失败'
  }
}

async function deleteSelectedPage() {
  const index = selectedPageIndex.value
  const page = currentPage.value
  if (index < 0 || !page || !activeProject.value) return
  if (!window.confirm(`确定删除第 ${index + 1} 页吗？此操作不可撤销。`)) return
  try {
    const response = await apiFetch(`/api/scan-projects/${activeProject.value.id}/pages/${page.id}`, { method: 'DELETE' })
    if (!response.ok) throw new Error(await readError(response))
    pages.value.splice(index, 1)
    URL.revokeObjectURL(page.url)
    selectedPageId.value = pages.value[index]?.id || pages.value[index - 1]?.id || ''
    await loadProjects()
    await initializeCropper()
  } catch (err) {
    error.value = err.message || '删除页面失败'
  }
}

function clearPages() {
  destroyCropper()
  for (const page of pages.value) URL.revokeObjectURL(page.url)
  pages.value = []
  selectedPageId.value = ''
}

async function loadProjects() {
  projectLoading.value = true
  try {
    const response = await apiFetch('/api/scan-projects')
    if (!response.ok) throw new Error(await readError(response))
    projects.value = await response.json()
  } catch (err) {
    error.value = err.message || '读取扫描项目失败'
  } finally {
    projectLoading.value = false
  }
}

function openNewProject() {
  newProjectName.value = ''
  showNewProject.value = true
}

async function openProjectHistory() {
  showProjectHistory.value = true
  await loadProjects()
}

async function createProject(nameOverride = newProjectName.value.trim()) {
  const name = nameOverride.trim()
  if (!name || projectSaving.value) return
  projectSaving.value = true
  error.value = ''
  try {
    if (activeProject.value && pages.value.length) await commitCurrentEditor()
    const response = await apiFetch('/api/scan-projects', { method: 'POST', body: JSON.stringify({ name }) })
    if (!response.ok) throw new Error(await readError(response))
    const project = await response.json()
    projects.value = [project, ...projects.value]
    clearPages()
    activeProject.value = project
    showNewProject.value = false
  } catch (err) {
    error.value = err.message || '创建扫描项目失败'
  } finally {
    projectSaving.value = false
  }
}

function defaultProjectName() {
  const now = new Date()
  const pad = value => String(value).padStart(2, '0')
  return `扫描项目 ${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`
}

async function createDefaultProject() {
  await createProject(defaultProjectName())
}

async function fetchProjectImage(url) {
  const response = await apiFetch(url)
  if (!response.ok) throw new Error(await readError(response))
  return response.blob()
}

async function loadProject(projectID) {
  if (projectLoading.value) return
  projectLoading.value = true
  error.value = ''
  try {
    if (activeProject.value && pages.value.length) await commitCurrentEditor()
    const response = await apiFetch(`/api/scan-projects/${projectID}`)
    if (!response.ok) throw new Error(await readError(response))
    const detail = await response.json()
    const loadedPages = await Promise.all(detail.pages.map(async page => {
      const blob = await fetchProjectImage(page.fileUrl)
      return {
        id: String(page.id),
        blob,
        originalBlob: undefined,
        originalUrl: page.originalUrl,
        url: URL.createObjectURL(blob)
      }
    }))
    clearPages()
    activeProject.value = detail.project
    pages.value = loadedPages
    selectedPageId.value = loadedPages[0]?.id || ''
    showProjectHistory.value = false
    await initializeCropper()
    await loadProjects()
  } catch (err) {
    error.value = err.message || '载入扫描项目失败'
  } finally {
    projectLoading.value = false
  }
}

async function deleteProject(project) {
  if (!window.confirm(`确定删除项目“${project.name}”及其中的全部页面吗？`)) return
  projectSaving.value = true
  try {
    const response = await apiFetch(`/api/scan-projects/${project.id}`, { method: 'DELETE' })
    if (!response.ok) throw new Error(await readError(response))
    projects.value = projects.value.filter(item => item.id !== project.id)
    if (activeProject.value?.id === project.id) {
      clearPages()
      activeProject.value = null
    }
  } catch (err) {
    error.value = err.message || '删除扫描项目失败'
  } finally {
    projectSaving.value = false
  }
}

function formatProjectTime(value) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

async function createProjectPage(blob) {
  const form = new FormData()
  form.append('image', new File([blob], 'scan.png', { type: 'image/png' }))
  const response = await apiFetch(`/api/scan-projects/${activeProject.value.id}/pages`, { method: 'POST', body: form })
  if (!response.ok) throw new Error(await readError(response))
  const page = await response.json()
  return {
    id: String(page.id),
    blob,
    originalBlob: blob,
    originalUrl: page.originalUrl,
    url: URL.createObjectURL(blob)
  }
}

async function persistEditedPage(page) {
  if (!activeProject.value) return
  const form = new FormData()
  form.append('image', new File([page.blob], 'scan.png', { type: 'image/png' }))
  const response = await apiFetch(`/api/scan-projects/${activeProject.value.id}/pages/${page.id}`, { method: 'PUT', body: form })
  if (!response.ok) throw new Error(await readError(response))
}

async function savePageOrder() {
  const response = await apiFetch(`/api/scan-projects/${activeProject.value.id}/pages/order`, {
    method: 'PUT',
    body: JSON.stringify({ pageIds: pages.value.map(page => Number(page.id)) })
  })
  if (!response.ok) throw new Error(await readError(response))
}

async function loadScanners(refresh = false) {
  loading.value = true
  error.value = ''
  try {
    const response = await apiFetch(`/api/scanners${refresh ? '?refresh=true' : ''}`)
    if (!response.ok) throw new Error(await readError(response))
    scanners.value = await response.json()
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
    if (done) throw new Error('扫描在完成前中断')
    const remaining = pending.subarray(offset)
    pending = new Uint8Array(remaining.length + value.length)
    pending.set(remaining)
    pending.set(value, remaining.length)
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
    for (let x = 0; x < metadata.width; x++) {
      const target = x * 4
      if (metadata.magic === 'P6') {
        const source = x * 3
        pixels[target] = raw[source]
        pixels[target + 1] = raw[source + 1]
        pixels[target + 2] = raw[source + 2]
      } else {
        const value = metadata.magic === 'P5' ? raw[x] : ((raw[Math.floor(x / 8)] & (0x80 >> (x % 8))) ? 0 : 255)
        pixels[target] = value
        pixels[target + 1] = value
        pixels[target + 2] = value
      }
      pixels[target + 3] = 255
    }
    context.putImageData(rowImage, 0, y)
    scanRows.value = y + 1
    if (y % 8 === 0) await new Promise(resolve => requestAnimationFrame(resolve))
  }
  const thumbnail = liveThumbnailCanvas.value
  if (thumbnail) {
    thumbnail.width = canvas.width
    thumbnail.height = canvas.height
    thumbnail.getContext('2d').drawImage(canvas, 0, 0)
  }
  return canvasToBlob(canvas)
}

async function scan() {
  if (!device.value || !activeProject.value || scanning.value) return
  destroyCropper()
  scanning.value = true
  error.value = ''
  scanRows.value = 0
  scanHeight.value = 0
  startScanTimer()
  scanAbortController = new AbortController()
  try {
    const response = await apiFetch('/api/scan/stream', {
      method: 'POST',
      signal: scanAbortController.signal,
      body: JSON.stringify({ device: device.value, mode: mode.value, resolution: resolution.value })
    })
    if (!response.ok) throw new Error(await readError(response))
    const page = await createProjectPage(await readScanStream(response))
    pages.value.push(page)
    selectedPageId.value = page.id
    await loadProjects()
  } catch (err) {
    if (err.name !== 'AbortError') error.value = err.message || '扫描失败'
  } finally {
    scanAbortController = undefined
    stopScanTimer()
    scanning.value = false
    await initializeCropper()
  }
}

function cancelScan() {
  scanAbortController?.abort()
}

function triggerDownload(url, filename, cleanup) {
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  cleanup?.()
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob)
  triggerDownload(url, filename, () => window.setTimeout(() => URL.revokeObjectURL(url), 1000))
}


async function pageToBlob(page, type) {
  if (type === 'image/png') return page.blob
  const bitmap = await createImageBitmap(page.blob)
  const canvas = document.createElement('canvas')
  canvas.width = bitmap.width
  canvas.height = bitmap.height
  canvas.getContext('2d', { alpha: false }).drawImage(bitmap, 0, 0)
  bitmap.close()
  return canvasToBlob(canvas, type, imageQuality.value)
}

async function exportImages() {
  try {
    await commitCurrentEditor()
    const type = imageFormat.value === 'jpeg' ? 'image/jpeg' : 'image/png'
    const extension = imageFormat.value === 'jpeg' ? 'jpg' : 'png'
    for (const [index, page] of pages.value.entries()) {
      downloadBlob(await pageToBlob(page, type), `scan-${String(index + 1).padStart(2, '0')}.${extension}`)
    }
  } catch (err) {
    error.value = err.message || '导出图片失败'
  }
}

async function exportPDF() {
  try {
    await commitCurrentEditor()
    const document = await PDFDocument.create()
    const [pageWidth, pageHeight] = [595.28, 841.89]
    const margin = 18
    for (const scanPage of pages.value) {
      const image = await document.embedPng(await scanPage.blob.arrayBuffer())
      const page = document.addPage([pageWidth, pageHeight])
      const scale = Math.min((pageWidth - margin * 2) / image.width, (pageHeight - margin * 2) / image.height)
      const width = image.width * scale
      const height = image.height * scale
      page.drawImage(image, {
        x: (pageWidth - width) / 2,
        y: (pageHeight - height) / 2,
        width,
        height
      })
    }
    triggerDownload(await document.saveAsBase64({ dataUri: true }), 'scans.pdf')
  } catch (err) {
    error.value = err.message || '导出 PDF 失败'
  }
}

function handleKeydown(event) {
  if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return
  if (event.key === 'Escape') cancelScan()
  if (event.key === '[') rotate(-90)
  if (event.key === ']') rotate(90)
  if (event.key === 'Delete') deleteSelectedPage()
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's' && pages.value.length) {
    event.preventDefault()
    exportPDF()
  }
}

onMounted(() => {
  loadScanners()
  createDefaultProject()
  window.addEventListener('keydown', handleKeydown)
})
onUnmounted(() => {
  cancelScan()
  stopScanTimer()
  clearPages()
  window.removeEventListener('keydown', handleKeydown)
})
</script>
