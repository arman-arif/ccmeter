import type { Period, Totals } from '../api';
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
