// Interface language. The Korean text is the key: t('새 콘솔') shows it as
// written in Korean and looks it up in the English table (i18n-en.ts) in
// English, so the components keep reading as the UI they draw. Missing
// entries fall back to the Korean text (npm run check:i18n lists them).
//
// Placeholders: t('{n}행', { n: 3 }); in English {n|row|rows} picks the
// singular when n is 1. A key can end in #context when one Korean word needs
// two English ones (t('연결#목록') shows 연결). t() reads the reactive language, so
// markup re-renders when it changes; text kept in variables must be made
// again (or kept as the Korean key and passed through t() where shown).

import en from './i18n-en'

export type Lang = 'ko' | 'en'
export const LANGS: { id: Lang; label: string }[] = [
  { id: 'ko', label: '한국어' },
  { id: 'en', label: 'English' },
]

function detect(): Lang {
  const l = (typeof navigator !== 'undefined' && (navigator.languages?.[0] || navigator.language)) || ''
  return l.toLowerCase().startsWith('ko') ? 'ko' : 'en'
}

export const i18n = $state({ lang: detect() })

export function t(ko: string, args?: Record<string, string | number>): string {
  let s = i18n.lang === 'ko' ? ko.replace(/#[^#]*$/, '') : (en[ko] ?? ko.replace(/#[^#]*$/, ''))
  if (args)
    s = s
      .replace(/\{(\w+)\|([^|}]*)\|([^}]*)\}/g, (m, k: string, one: string, many: string) =>
        k in args ? (String(args[k]) === '1' ? one : many) : m,
      )
      .replace(/\{(\w+)\}/g, (m, k: string) => (k in args ? String(args[k]) : m))
  return s
}

// Numbers and dates in the interface language.
export const locale = () => (i18n.lang === 'ko' ? 'ko-KR' : 'en-US')
export const num = (n: number) => n.toLocaleString(locale())

export function isLang(v: unknown): v is Lang {
  return v === 'ko' || v === 'en'
}

// For values put into a translated string shown with {@html}.
export const esc = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)
