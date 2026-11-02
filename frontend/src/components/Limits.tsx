import type { ReactNode } from 'react';
import type { Block, LimitWindow, PlanLimits } from '../api';
import { dur, hasTime, hhmm, tok, untilText, usd } from '../format';
import { Icon } from './Icon';

const severity = (pct: number) => (pct >= 100 ? 'over' : pct >= 80 ? 'near' : '');

function Meter({ pct, label, marker }: { pct: number; label?: string; marker?: number }) {
  const fill = Math.max(0, Math.min(pct, 100));
  return (
    <div
      className={`bar ${severity(pct)}`}
      role="progressbar"
      aria-label={label}
      aria-valuenow={Math.round(fill)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <span style={{ width: `${fill}%` }} />
      {marker !== undefined && marker > fill && (
        <i style={{ left: `calc(${marker}% - 1px)` }} title="Projected at current pace" />
      )}
    </div>
  );
}

function PlanWindow({ w, label, icon, estimate }: { w: LimitWindow; label: string; icon: string; estimate?: ReactNode }) {
  if (w.reset) {
    return (
      <div className="plan">
        <div className="plan-row">
          <span className="v">0%</span>
          <span className="k">{label}</span>
          <span className="right">
            <Icon name="refresh" />
            reset since last report
          </span>
        </div>
        {estimate}
      </div>
    );
  }
  return (
    <div className="plan">
      <div className="plan-row">
        <span className="v">{Math.round(w.used_pct)}%</span>
        <span className="k">{label}</span>
        {hasTime(w.resets_at) && (
          <span className="right">
            <Icon name={icon} />
            {untilText(w.resets_at)}
          </span>
        )}
      </div>
      <Meter pct={w.used_pct} label={label} />
      {estimate}
      {w.used_pct >= 80 && (
        <div className="block-meta">
          <span className="warn">
            <Icon name="alert" />
            {w.used_pct >= 100 ? 'Limit reached' : 'Near limit'}
          </span>
        </div>
      )}
    </div>
  );
}

const SOURCES: Record<string, string> = {
  'claude-cli': 'From claude /usage',
  'usage-endpoint': 'From your plan usage',
};

export const planSource = (source: string) => SOURCES[source] ?? 'Reported by the Claude Code status line';

// Real plan limits, as last reported by Claude Code.
export function PlanBars({ plan, block }: { plan: PlanLimits; block: Block }) {
  const estimate = block.active && (
    <div className="block-meta">
      <span>{usd(block.cost_usd)} est</span>
      <span>{tok(block.tokens)} tokens</span>
      <span>
        <Icon name="trend" /> {usd(block.burn_usd_per_hour)}/h
      </span>
    </div>
  );
  return (
    <>
      {plan.five_hour && <PlanWindow w={plan.five_hour} label="of 5-hour plan limit" icon="clock" estimate={estimate} />}
      {plan.seven_day && <PlanWindow w={plan.seven_day} label="of weekly plan limit" icon="calendar" />}
      <div className="plan-src">
        <Icon name="info" />
        {planSource(plan.source)}, updated{' '}
        {plan.age_min < 1 ? 'just now' : dur(plan.age_min) + ' ago'}
      </div>
    </>
  );
}

// The 5-hour window estimated from local logs.
export function BlockEstimate({ block: b }: { block: Block }) {
  if (!b.active) {
    return (
      <div className="block">
        <div className="idle">
          <Icon name="pause" />
          No active 5-hour window
        </div>
      </div>
    );
  }
  const hasLimit = b.limit_usd > 0;
  const projected = hasLimit ? Math.min((100 * b.projected_usd) / b.limit_usd, 100) : undefined;
  const limitLabel = b.limit_source === 'config' ? 'of limit' : 'of 30-day peak';
  return (
    <div className="block">
      <div className="block-top">
        <span className="v">{usd(b.cost_usd)}</span>
        <span className="k">{hasLimit ? `${Math.round(b.pct)}% ${limitLabel}` : '5-hour window'}</span>
        <span className="right">
          <Icon name="clock" />
          {dur(b.remaining_min)} left
        </span>
      </div>
      {hasLimit ? <Meter pct={b.pct} marker={projected} /> : <div style={{ height: 8 }} />}
      <div className="block-meta">
        <span>{tok(b.tokens)} tokens</span>
        <span>
          <Icon name="trend" /> {usd(b.burn_usd_per_hour)}/h
        </span>
        <span>projected {usd(b.projected_usd)}</span>
        <span>
          {hhmm(b.start)} – {hhmm(b.end)}
        </span>
        {hasLimit && b.pct >= 80 && (
          <span className="warn">
            <Icon name="alert" />
            {b.pct >= 100 ? (b.limit_source === 'peak' ? 'New peak' : 'Over limit') : 'Near limit'}
          </span>
        )}
      </div>
    </div>
  );
}

export function hasPlan(plan?: PlanLimits): plan is PlanLimits {
  return !!plan && (!!plan.five_hour || !!plan.seven_day);
}
