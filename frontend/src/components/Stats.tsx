import type { ReactNode } from 'react';
import type { AccountInfo, Period, Totals } from '../api';
import { count, tok, usd } from '../format';
import { Icon } from './Icon';

export function Tile({ label, t, note }: { label: string; t: Totals; note?: string }) {
  return (
    <div className="tile">
      <div className="label">{label}</div>
      <div className="value">{usd(t.cost_usd)}</div>
      <div className="sub">
        {tok(t.tokens)} tokens · {count(t.messages)} responses
        {note && ` · ${note}`}
      </div>
    </div>
  );
}

// One measure over every period: the all-time figure, then the shorter ones.
export function CountTile(props: {
  label: string;
  periods: { today: Totals; week: Totals; month: Totals; all: Totals };
  value: (t: Totals) => number;
  format: (n: number) => string;
}) {
  const { label, periods, value, format } = props;
  return (
    <div className="tile">
      <div className="label">{label}, all time</div>
      <div className="value">{format(value(periods.all))}</div>
      <div className="sub">
        Today {format(value(periods.today))} · 7 days {format(value(periods.week))} · 30 days {format(value(periods.month))}
      </div>
    </div>
  );
}

export function PeriodStat({ label, p }: { label: string; p: Period }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v">{usd(p.cost_usd)}</div>
      <div className="s">
        {tok(p.tokens)}
        {!!p.limit_usd && ` · ${Math.round(p.pct ?? 0)}%`}
      </div>
    </div>
  );
}

export function DirLine({ dir }: { dir: string }) {
  return (
    <div className="dir" title={dir}>
      <Icon name="folder" />
      <span>{dir}</span>
    </div>
  );
}

export function Section({ title, sub, children }: { title: string; sub?: ReactNode; children: ReactNode }) {
  return (
    <section className="card section">
      <div className="section-head">
        <h3>{title}</h3>
        {sub && <span className="section-sub">{sub}</span>}
      </div>
      {children}
    </section>
  );
}

export function PlanTag({ info }: { info?: AccountInfo }) {
  return info?.plan ? <span className="tag">{info.plan}</span> : null;
}

export function EmailLine({ info }: { info?: AccountInfo }) {
  if (!info?.email) return null;
  return (
    <div className="dir email-line">
      <Icon name="users" />
      <span title={info.email}>{info.email}</span>
    </div>
  );
}
