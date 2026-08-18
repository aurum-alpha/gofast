/** Deterministic Guide block colors from a canonical category name. */

const PALETTE: ReadonlyArray<{ bg: string; accent: string }> = [
  { bg: '#1e3a5f', accent: '#4da3ff' }, // blue
  { bg: '#3d1f4a', accent: '#d46cff' }, // purple
  { bg: '#1f4a2e', accent: '#3dcc6e' }, // green
  { bg: '#4a3514', accent: '#f0a832' }, // amber
  { bg: '#4a1f24', accent: '#ff5c6a' }, // red
  { bg: '#14444a', accent: '#2ad4e0' }, // cyan
  { bg: '#3a2a14', accent: '#e8c04a' }, // gold
  { bg: '#2a3a18', accent: '#8fd14a' }, // lime
  { bg: '#3a1840', accent: '#e070c0' }, // magenta
  { bg: '#183448', accent: '#6ab0e8' }, // steel
  { bg: '#4a2818', accent: '#ff8a4a' }, // orange
  { bg: '#243018', accent: '#70b060' }, // olive
]

const DEFAULT = {
  background: '#262a33',
  borderColor: 'var(--line)',
  borderLeft: '3px solid var(--line)',
}

/** CSS-safe slug for epg-cat-* class names. */
export function categorySlug(name: string): string {
  const s = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return s || 'uncategorized'
}

function hashName(name: string): number {
  let h = 0
  const key = name.trim().toLowerCase()
  for (let i = 0; i < key.length; i++) {
    h = (h * 31 + key.charCodeAt(i)) >>> 0
  }
  return h
}

/** Inline style bits for an epg-prog block from its first category. */
export function categoryStyle(name: string | undefined | null): {
  background: string
  borderColor: string
  borderLeft: string
} {
  const n = name?.trim()
  if (!n) return DEFAULT
  const swatch = PALETTE[hashName(n) % PALETTE.length]
  return {
    background: swatch.bg,
    borderColor: swatch.accent,
    borderLeft: `3px solid ${swatch.accent}`,
  }
}

/** Prefer emitted_categories, else categories (matches Go ExportCategories). */
export function exportCategories(p: {
  categories?: string[] | null
  emitted_categories?: string[] | null
}): string[] {
  if (p.emitted_categories && p.emitted_categories.length > 0) {
    return p.emitted_categories
  }
  return p.categories ?? []
}
