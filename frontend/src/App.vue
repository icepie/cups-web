<template>
  <UApp>
    <div v-if="!sessionLoaded" class="flex items-center justify-center min-h-screen bg-default">
      <UIcon name="i-lucide-loader-circle" class="w-8 h-8 animate-spin text-primary" />
    </div>
    <div v-else class="grid grid-rows-[auto_1fr_auto] min-h-screen w-full bg-default">
      <header class="flex items-center justify-between px-4 sm:px-6 py-3 border-b border-default bg-default">
        <div class="flex items-center gap-3 min-w-0">
          <h1 class="text-xl font-bold shrink-0">CUPS 打印</h1>
          <span v-if="session" class="text-sm text-muted truncate">{{ session.username }}</span>
        </div>
        <div class="flex items-center gap-2">
          <!-- 桌面端（sm+）：分段按钮 + 文字，一目了然 -->
          <div class="hidden sm:flex items-center gap-2">
            <!-- 导航分段容器：与主 CTA 视觉区分 -->
            <UButton
              v-if="session"
              :variant="route.path === '/scan' ? 'soft' : 'ghost'"
              :color="route.path === '/scan' ? 'primary' : 'neutral'"
              size="xs"
              icon="i-lucide-scan-line"
              @click="router.push('/scan')"
            >
              扫描
            </UButton>
            <div
              v-if="isAdmin"
              class="flex items-center gap-0.5 p-0.5 rounded-lg bg-elevated/60 border border-default"
            >
              <UButton
                :variant="route.path === '/print' ? 'soft' : 'ghost'"
                :color="route.path === '/print' ? 'primary' : 'neutral'"
                size="xs"
                icon="i-lucide-file-text"
                @click="router.push('/print')"
              >
                打印
              </UButton>
              <UButton
                :variant="route.path === '/admin' ? 'soft' : 'ghost'"
                :color="route.path === '/admin' ? 'primary' : 'neutral'"
                size="xs"
                icon="i-lucide-settings"
                @click="router.push('/admin')"
              >
                管理
              </UButton>
              <UButton
                :variant="route.path === '/drivers' ? 'soft' : 'ghost'"
                :color="route.path === '/drivers' ? 'primary' : 'neutral'"
                size="xs"
                icon="i-lucide-puzzle"
                @click="router.push('/drivers')"
              >
                驱动
              </UButton>
            </div>
            <UButton
              v-if="session"
              variant="ghost"
              color="neutral"
              size="xs"
              icon="i-lucide-log-out"
              @click="logout"
            >
              登出
            </UButton>
          </div>
          <!-- 移动端（<sm）：折叠为汉堡菜单，图标+文字，易点易读 -->
          <UDropdownMenu
            v-if="session"
            :items="menuItems"
            :content="{ align: 'end' }"
            class="sm:hidden"
          >
            <UButton variant="ghost" color="neutral" size="sm" icon="i-lucide-menu" square />
          </UDropdownMenu>
        </div>
      </header>
      <div class="overflow-auto relative">
        <router-view :session="session" @login-success="onLogin" @logout="onLogout" />
      </div>
      <footer class="px-6 py-3 border-t border-default bg-default text-sm text-muted flex items-center justify-center gap-3 flex-wrap">
        <span>
          Powered by
          <a href="https://github.com/icepie/cups-web" target="_blank" rel="noopener" class="text-primary hover:underline">cups-web</a>
        </span>
        <span v-if="appVersion" class="text-default/40">·</span>
        <!-- 版本号：二进制构建期由 -ldflags 注入到 main.Version，经 /api/version 返回。 -->
        <span v-if="appVersion" class="font-mono text-xs" :title="`cups-web ${appVersion}`">
          {{ appVersion }}
        </span>
      </footer>
    </div>

  </UApp>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { clearSessionCache, updateSessionCache } from './router'

const router = useRouter()
const route = useRoute()

const session = ref(null)
const sessionLoaded = ref(false)
const showSponsorModal = ref(false)
// 二进制版本号：首次挂载时拉一次 /api/version（公开接口，不要求登录），
// 失败时保持空字符串，footer 上的版本号节点会被 v-if 隐藏，不影响布局。
const appVersion = ref('')

const isAdmin = computed(() => session.value?.role === 'admin')

// 移动端汉堡菜单项：导航项（仅 admin）与登出分成两组，组间自动加分隔线
const menuItems = computed(() => {
  const nav = []
  nav.push({ label: '扫描', icon: 'i-lucide-scan-line', onSelect: () => router.push('/scan') })
  if (isAdmin.value) {
    nav.push({ label: '打印', icon: 'i-lucide-file-text', onSelect: () => router.push('/print') })
    nav.push({ label: '管理', icon: 'i-lucide-settings', onSelect: () => router.push('/admin') })
    nav.push({ label: '驱动', icon: 'i-lucide-puzzle', onSelect: () => router.push('/drivers') })
  }
  const account = [{ label: '登出', icon: 'i-lucide-log-out', onSelect: () => logout() }]
  return nav.length ? [nav, account] : [account]
})

async function loadVersion() {
  try {
    const resp = await fetch('/api/version', { credentials: 'include' })
    if (resp.ok) {
      const data = await resp.json()
      if (data && typeof data.version === 'string') {
        appVersion.value = data.version
      }
    }
  } catch (e) {
    // 版本号展示属于可降级的信息，静默失败即可
  }
}

async function loadSession() {
  try {
    const resp = await fetch('/api/session', { credentials: 'include' })
    if (resp.ok) {
      const data = await resp.json()
      session.value = data
      updateSessionCache(data)
      router.push('/print')
    } else {
      session.value = null
      router.push('/login')
    }
  } catch (e) {
    session.value = null
  } finally {
    sessionLoaded.value = true
  }
}

function onLogin() {
  loadSession()
}

function onLogout() {
  session.value = null
  clearSessionCache()
  router.push('/login')
}

async function logout() {
  try {
    await fetch('/api/logout', { method: 'POST', credentials: 'include' })
  } catch (e) {
    // ignore errors
  }
  onLogout()
}

function detectOS() {
  if (navigator.userAgent.indexOf('Windows') !== -1) {
    document.documentElement.classList.add('is-windows')
  }
}

onMounted(() => {
  detectOS()
  loadSession()
  loadVersion()
})
</script>
