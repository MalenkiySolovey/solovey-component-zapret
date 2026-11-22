import { registerComponent, unregisterComponent } from '@solovey/component-ui/contributions'

export function register() {
  registerComponent({
    id: 'zapret', apiVersion: '1.0',
    routes: [{ path: '/zapret', name: 'component.zapret', meta: { title: 'zapretCatalog.title' }, component: () => import('./Strategies.vue') }],
    nav: [{ title: 'zapretCatalog.title', icon: 'mdi-tune-variant', nexusIcon: 'lucide:sliders-horizontal', route: 'component.zapret', order: 34, section: 'proxy' }],
    locales: { en: () => import('./locales/en'), ru: () => import('./locales/ru') },
  })
}

export function unregister() { unregisterComponent('zapret') }
