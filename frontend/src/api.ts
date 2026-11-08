// Types mirror the JSON from server.go, stats.go, limits.go, detail.go and prices.go.

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

export interface ModelLimit extends LimitWindow {
  name: string;
}

export interface PlanLimits {
  source: string;
  updated_at: string;
  age_min: number;
  five_hour?: LimitWindow;
  seven_day?: LimitWindow;
  models?: ModelLimit[];
}

export interface AccountInfo {
  email?: string;
  name?: string;
  organization?: string;
  plan?: string;
  role?: string;
  extra_usage: boolean;
  account_created?: string;
  subscription_since?: string;
  profile_updated?: string;
}

export interface Summary {
  name: string;
  dir: string;
  archived?: boolean;
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
  account?: AccountInfo;
  plan?: PlanLimits;
  plan_error?: string;
}

export interface Report {
  generated_at: string;
  accounts: Summary[] | null;
  archived?: Summary[] | null;
  total: { today: Totals; week: Totals; month: Totals };
}

export interface AccountView {
  name: string;
  dir: string;
  block_limit_usd?: number;
  weekly_limit_usd?: number;
  archived?: boolean;
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

export interface ProjectRow extends Totals {
  path: string;
  sessions: number;
  last_active: string;
}

export interface SessionRow extends Totals {
  id: string;
  project: string;
  models: string[] | null;
  start: string;
  last: string;
}

export interface BlockRow extends Totals {
  start: string;
  end: string;
  last: string;
  active: boolean;
}

export interface HourCell extends Totals {
  weekday: number;
  hour: number;
}

export interface RangeDetail extends Totals {
  days: number;
  from: string;
  to: string;
  daily: DayPoint[];
  models: ModelRow[];
  projects: ProjectRow[];
  sessions: SessionRow[];
  session_count: number;
  blocks: BlockRow[];
  hours: HourCell[];
}

export interface AccountSetup {
  hooks: boolean;
  plugin: boolean;
  status_line: boolean;
  settings_error?: string;
  last_attempt?: string;
  last_ok?: string;
  next_allowed?: string;
  last_error?: string;
}

export interface AccountDetail extends Summary {
  all_time?: Totals & { since: string };
  setup: AccountSetup;
  range: RangeDetail;
}

export interface PriceView {
  input: number;
  output: number;
  cache_write: number;
  cache_write_1h: number;
  cache_read: number;
}

export interface ListedPrice {
  name: string;
  price: PriceView;
}

export interface UsedPrice {
  model: string;
  source: '' | 'override' | 'builtin';
  match?: string;
  price?: PriceView;
  accounts: string[];
  messages: number;
  tokens: number;
  cost_usd: number;
}

export interface PricesReport {
  checked_on: string;
  source_url: string;
  config_path: string;
  used: UsedPrice[];
  overrides: ListedPrice[];
  builtin: ListedPrice[];
}
