export function tok(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'k';
  return String(n);
}

export const usd = (v: number) => '$' + (v >= 100 ? v.toFixed(0) : v.toFixed(2));

export const count = (n: number) => n.toLocaleString();

export const dur = (m: number) =>
  m < 60 ? m + 'm' : Math.floor(m / 60) + 'h ' + String(m % 60).padStart(2, '0') + 'm';

export const hhmm = (t: string) =>
  new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

export const dayTime = (t: string) =>
  new Date(t).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' });

export function ago(t: string): string {
  const s = (Date.now() - new Date(t).getTime()) / 1000;
  if (s < 90) return 'just now';
  if (s < 3600) return Math.round(s / 60) + 'm ago';
  if (s < 86400) return Math.round(s / 3600) + 'h ago';
  return Math.round(s / 86400) + 'd ago';
}

export function untilText(t: string): string {
  const m = Math.max(0, Math.round((new Date(t).getTime() - Date.now()) / 60000));
  return m < 24 * 60 ? dur(m) + ' left' : 'resets ' + dayTime(t);
}

// Go's zero time, which means "not set".
export const hasTime = (t?: string): t is string => !!t && !t.startsWith('0001');

// A YYYY-MM-DD date, read as local time so it doesn't shift across midnight.
const localDate = (d: string) => new Date(d + 'T00:00:00');

export const longDate = (d: string) =>
  localDate(d).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' });

export const shortDate = (d: string) =>
  localDate(d).toLocaleDateString([], { month: 'short', day: 'numeric' });

// The smallest clean number at or above v, for the top of a chart's axis.
export function niceMax(v: number): number {
  if (v <= 0) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}

export const dateTime = (t: string) =>
  new Date(t).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

// The local day of a full timestamp.
export const shortDay = (t: string) => new Date(t).toLocaleDateString([], { month: 'short', day: 'numeric' });

export const percent = (part: number, whole: number) => (whole > 0 ? Math.round((100 * part) / whole) + '%' : '0%');

// The last folder of a path, on any OS. Log folder names have no separators
// and come back whole.
export const baseName = (path: string) => path.split(/[\\/]/).filter(Boolean).pop() ?? path;

export const shortModel = (m: string) => m.replace(/^claude-/, '');
