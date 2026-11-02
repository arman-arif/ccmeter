// Types mirror the JSON from server.go, stats.go, limits.go and detail.go.

export interface Totals {
  input: number;
  output: number;
  cache_write: number;
  cache_read: number;
  tokens: number;
  cost_usd: number;
  messages: number;
}

export interface Block extends Totals {
  active: boolean;
  start: string;
  end: string;
  remaining_min: number;
  burn_usd_per_hour: number;
  projected_usd: number;
  limit_usd: number;
  limit_source: '' | 'config' | 'peak';
  pct: number;
}

export interface Period extends Totals {
  limit_usd?: number;
  pct?: number;
}

export interface DayPoint extends Totals {
  date: string;
}

export interface ModelRow extends Totals {
  model: string;
  priced: boolean;
}

export interface LimitWindow {
  used_pct: number;
  resets_at: string;
  reset?: boolean;
}

export interface PlanLimits {
  source: string;
  updated_at: string;
  age_min: number;
  five_hour?: LimitWindow;
  seven_day?: LimitWindow;
}

export interface Summary {
  name: string;
  dir: string;
  found: boolean;
  error?: string;
  files: number;
  last_activity?: string;
  block: Block;
  today: Period;
  week: Period;
  month: Period;
  daily: DayPoint[] | null;
  models: ModelRow[] | null;
  unpriced?: string[];
  plan?: PlanLimits;
  plan_error?: string;
}

export interface Report {
  generated_at: string;
  accounts: Summary[] | null;
  total: { today: Totals; week: Totals; month: Totals };
}

export interface AccountView {
  name: string;
  dir: string;
  block_limit_usd?: number;
  weekly_limit_usd?: number;
  has_projects: boolean;
}

export interface AccountsResponse {
  config_path: string;
  from_file: boolean;
  editable: boolean;
  accounts: AccountView[] | null;
}

export interface SaveAccount {
  original: string;
  name: string;
  dir: string;
  block_limit_usd: number;
  weekly_limit_usd: number;
}

export async function api<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    cache: 'no-store',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const out = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(out.error || res.statusText);
  return out as T;
}
