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
    { path: '/', name: 'home', component: () => import('@/views/HomeView.vue') },
    {
      path: '/tournaments/:id/setup',
      name: 'setup',
      component: () => import('@/views/SetupView.vue'),
    },
    {
      path: '/tournaments/:id/schedule',
      name: 'schedule',
      component: () => import('@/views/ScheduleView.vue'),
    },
    {
      path: '/tournaments/:id/review',
      name: 'review',
      component: () => import('@/views/ReviewView.vue'),
    },
    { path: '/referee', name: 'referee', component: () => import('@/views/RefereeView.vue') },
    { path: '/referee/:id', name: 'match', component: () => import('@/views/MatchView.vue') },
    {
      path: '/public/:abbrev',
      name: 'public',
      component: () => import('@/views/PublicView.vue'),
      meta: { public: true },
    },
    {
      path: '/display/:abbrev',
      name: 'display',
      component: () => import('@/views/DisplayView.vue'),
      meta: { public: true },
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
router.beforeEach(async (to) => {
  if (to.meta.public) return true
  try {
    return (await checkSession()) ? true : { name: 'login', query: { returnTo: to.fullPath } }
  } catch {
    return { name: 'login', query: { error: 'unavailable', returnTo: to.fullPath } }
  }
})
export default router
