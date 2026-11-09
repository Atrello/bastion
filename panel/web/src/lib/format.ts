// Small formatting helpers shared by the pages.

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

/** 9_771_000_000 → "9.1 GB". */
export function bytes(n: number): string {
  let i = 0;
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${UNITS[i]}`;
}

/** 273_600 → "3d 4h"; 3_000 → "50m". */
export function duration(secs: number): string {
  const d = Math.floor(secs / 86400);
  const h = Math.floor((secs % 86400) / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

/** How long ago, for "last seen": "just now", "4 min ago", "2 h ago", or the date. */
export function ago(iso: string): string {
  const secs = (Date.now() - Date.parse(iso)) / 1000;
  if (secs < 60) return 'just now';
  if (secs < 3600) return `${Math.floor(secs / 60)} min ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)} h ago`;
  return new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' });
}

export const percent = (used: number, total: number) => (total ? Math.round((used / total) * 100) : 0);
