import { checkSession } from '@/lib/auth'
import { createRouter, createWebHashHistory } from 'vue-router'

const router = createRouter({
  history: createWebHashHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true },
    },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  try {
    if (await checkSession()) return true
    return { name: 'login', query: { returnTo: to.fullPath } }
  } catch {
    return { name: 'login', query: { error: 'unavailable', returnTo: to.fullPath } }
  }
})

export default router
