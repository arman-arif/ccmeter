import type { ReactNode } from 'react';
import type { AccountDetail, AccountInfo, BlockRow, DayPoint, HourCell, ModelRow, ProjectRow, RangeDetail, SessionRow, Totals } from '../api';
import { BarChart } from '../components/BarChart';
import { Heatmap, hourLabel, WEEKDAYS } from '../components/Heatmap';
import { Icon } from '../components/Icon';
import { BlockEstimate, hasPlan, PlanBars, planSource } from '../components/Limits';
import { DirLine, EmailLine, PlanTag, Section, Tile } from '../components/Stats';
import { Segmented, Table, TableView, type Column } from '../components/Table';
import { ago, baseName, count, dateTime, fullDay, hasTime, hhmm, longDate, percent, shortDate, shortDay, shortModel, tok, usd } from '../format';
import { usePoll, useTitle } from '../hooks';
import { Link, navigate, useUrl } from '../router';
import { NotFound } from './NotFound';

type MetricKey = 'cost' | 'tokens' | 'responses';

interface Metric {
  label: string;
  value: (t: Totals) => number;
  format: (v: number) => string;
  integer?: boolean;
}

const METRICS: Record<MetricKey, Metric> = {
  cost: { label: 'Cost', value: t => t.cost_usd, format: usd },
  tokens: { label: 'Tokens', value: t => t.tokens, format: tok },
  responses: { label: 'Responses', value: t => t.messages, format: count, integer: true },
};

// The figures a tooltip adds next to the one being charted.
function otherFigures(key: MetricKey, t: Totals): string {
  const parts = [];
  if (key !== 'cost') parts.push(usd(t.cost_usd));
  if (key !== 'tokens') parts.push(tok(t.tokens) + ' tokens');
  if (key !== 'responses') parts.push(count(t.messages) + ' responses');
  return '· ' + parts.join(' · ');
}

const tokenColumns = <T extends Totals>(): Column<T>[] => [
  { head: 'Input', cell: r => tok(r.input) },
  { head: 'Output', cell: r => tok(r.output) },
  { head: 'Cache W', cell: r => tok(r.cache_write) },
  { head: 'Cache R', cell: r => tok(r.cache_read) },
];

function Stat({ label, value, sub }: { label: string; value: string; sub: string }) {
  return (
    <div className="tile small">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
      <div className="sub">{sub}</div>
    </div>
  );
}

function RangeStats({ r }: { r: RangeDetail }) {
  const active = r.daily.filter(d => d.messages > 0);
  const busiest = active.reduce<DayPoint | null>((best, d) => (!best || d.cost_usd > best.cost_usd ? d : best), null);
  const cacheable = r.input + r.cache_write + r.cache_read;
  return (
    <section className="stats">
      <Stat label="Cost in this range" value={usd(r.cost_usd)} sub={`${tok(r.tokens)} tokens`} />
      <Stat
        label="Responses"
        value={count(r.messages)}
        sub={active.length ? `about ${count(Math.round(r.messages / active.length))} per active day` : 'none yet'}
      />
      <Stat label="Sessions" value={count(r.session_count)} sub={`across ${r.projects.length} project${r.projects.length === 1 ? '' : 's'}`} />
      <Stat
        label="Active days"
        value={`${active.length} of ${r.days}`}
        sub={busiest ? `busiest ${shortDate(busiest.date)}, ${usd(busiest.cost_usd)}` : 'none yet'}
      />
      <Stat label="Read from cache" value={percent(r.cache_read, cacheable)} sub="of input tokens" />
      <Stat label="Cost per response" value={usd(r.messages ? r.cost_usd / r.messages : 0)} sub={`${tok(r.messages ? Math.round(r.tokens / r.messages) : 0)} tokens each`} />
    </section>
  );
}

function DailySection({ r, metricKey }: { r: RangeDetail; metricKey: MetricKey }) {
  const m = METRICS[metricKey];
  const rows = r.daily.filter(d => d.messages > 0).reverse();
  const columns: Column<DayPoint>[] = [
    { head: 'Date', left: true, cell: d => longDate(d.date) },
    ...tokenColumns<DayPoint>(),
    { head: 'Tokens', cell: d => tok(d.tokens) },
    { head: 'Responses', cell: d => count(d.messages) },
    { head: 'Cost', cell: d => usd(d.cost_usd) },
  ];
  return (
    <Section title={`Daily ${m.label.toLowerCase()}`}>
      <BarChart
        label={`Daily ${m.label.toLowerCase()}, ${shortDate(r.from)} to ${shortDate(r.to)}`}
        height={180}
        format={m.format}
        integer={m.integer}
        firstLabel={shortDate(r.from)}
        lastLabel={shortDate(r.to)}
        bars={r.daily.map(d => ({ key: d.date, value: m.value(d), title: longDate(d.date), detail: otherFigures(metricKey, d) }))}
      />
      {rows.length > 0 && (
        <TableView title="Daily table">
          <Table columns={columns} rows={rows} rowKey={d => d.date} limit={31} />
        </TableView>
      )}
    </Section>
  );
}

function TokenMix({ r }: { r: RangeDetail }) {
  const rows = [
    { name: 'Cache reads', v: r.cache_read },
    { name: 'Cache writes', v: r.cache_write },
    { name: 'Output', v: r.output },
    { name: 'Input', v: r.input },
  ].sort((a, b) => b.v - a.v);
  const max = Math.max(1, ...rows.map(x => x.v));
  return (
    <Section title="Token types" sub={`${tok(r.tokens)} in all`}>
      <table className="mix">
        <tbody>
          {rows.map(x => (
            <tr key={x.name}>
              <td className="left">{x.name}</td>
              <td className="mix-bar">
                <div className="hbar" style={{ width: `${(100 * x.v) / max}%` }} />
              </td>
              <td>{tok(x.v)}</td>
              <td>{percent(x.v, r.tokens)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Section>
  );
}

function ActivitySection({ r, metricKey }: { r: RangeDetail; metricKey: MetricKey }) {
  const m = METRICS[metricKey];
  const columns: Column<HourCell>[] = [
    { head: 'Day', left: true, cell: c => WEEKDAYS[c.weekday] },
    { head: 'Hour', left: true, cell: c => hourLabel(c.hour) },
    { head: 'Tokens', cell: c => tok(c.tokens) },
    { head: 'Responses', cell: c => count(c.messages) },
    { head: 'Cost', cell: c => usd(c.cost_usd) },
  ];
  return (
    <Section title="When you use it" sub="by weekday and hour, this machine's time">
      <Heatmap
        label={`${m.label} by weekday and hour`}
        format={m.format}
        cells={r.hours.map(c => ({ weekday: c.weekday, hour: c.hour, value: m.value(c), detail: otherFigures(metricKey, c) }))}
      />
      {r.hours.length > 0 && (
        <TableView title="Hours table">
          <Table columns={columns} rows={r.hours} rowKey={c => `${c.weekday}:${c.hour}`} />
        </TableView>
      )}
    </Section>
  );
}

function WindowsSection({ r, metricKey }: { r: RangeDetail; metricKey: MetricKey }) {
  const m = METRICS[metricKey];
  const oldestFirst = [...r.blocks].reverse();
  const busiest = r.blocks.reduce<BlockRow | null>((best, b) => (!best || m.value(b) > m.value(best) ? b : best), null);
  const columns: Column<BlockRow>[] = [
    {
      head: 'Started',
      left: true,
      cell: b => (
        <>
          {dateTime(b.start)}
          {b.active && <span className="tag">now</span>}
        </>
      ),
    },
    { head: 'Last response', left: true, cell: b => hhmm(b.last) },
    { head: 'Tokens', cell: b => tok(b.tokens) },
    { head: 'Responses', cell: b => count(b.messages) },
    { head: 'Cost', cell: b => usd(b.cost_usd) },
  ];
  return (
    <Section title="5-hour windows" sub={busiest && `busiest ${m.format(m.value(busiest))} on ${dateTime(busiest.start)}`}>
      {r.blocks.length === 0 ? (
        <div className="idle">
          <Icon name="pause" />
          No windows in this range
        </div>
      ) : (
        <>
          <BarChart
            label={`${m.label} per 5-hour window`}
            format={m.format}
            integer={m.integer}
            firstLabel={shortDay(oldestFirst[0].start)}
            lastLabel={shortDay(oldestFirst[oldestFirst.length - 1].start)}
            bars={oldestFirst.map(b => ({ key: b.start, value: m.value(b), title: dateTime(b.start), detail: otherFigures(metricKey, b) }))}
          />
          <TableView title="Windows table">
            <Table columns={columns} rows={r.blocks} rowKey={b => b.start} />
          </TableView>
        </>
      )}
    </Section>
  );
}

function ModelsSection({ r }: { r: RangeDetail }) {
  const columns: Column<ModelRow>[] = [
    { head: 'Model', left: true, cell: m => <span title={m.model}>{shortModel(m.model)}</span> },
    { head: 'Responses', cell: m => count(m.messages) },
    ...tokenColumns<ModelRow>(),
    { head: 'Cost', cell: m => (m.priced ? usd(m.cost_usd) : 'unpriced') },
    { head: 'Share', cell: m => (m.priced ? percent(m.cost_usd, r.cost_usd) : '') },
  ];
  return (
    <Section title="Models" sub={<Link href="/prices">See prices</Link>}>
      <Table columns={columns} rows={r.models} rowKey={m => m.model} />
    </Section>
  );
}

function ProjectsSection({ r }: { r: RangeDetail }) {
  const columns: Column<ProjectRow>[] = [
    {
      head: 'Project',
      left: true,
      cell: p => (
        <div className="cell-path" title={p.path}>
          <div>{baseName(p.path)}</div>
          <div className="cell-sub">{p.path}</div>
        </div>
      ),
    },
    { head: 'Sessions', cell: p => count(p.sessions) },
    { head: 'Responses', cell: p => count(p.messages) },
    { head: 'Tokens', cell: p => tok(p.tokens) },
    { head: 'Last active', cell: p => ago(p.last_active) },
    { head: 'Cost', cell: p => usd(p.cost_usd) },
    { head: 'Share', cell: p => percent(p.cost_usd, r.cost_usd) },
  ];
  return (
    <Section title="Projects" sub="by the folder each session started in">
      <Table columns={columns} rows={r.projects} rowKey={p => p.path} />
    </Section>
  );
}

function SessionsSection({ r }: { r: RangeDetail }) {
  const columns: Column<SessionRow>[] = [
    {
      head: 'Session',
      left: true,
      cell: s => (
        <div className="cell-path" title={`${s.id}\n${s.project}`}>
          <div>{baseName(s.project)}</div>
          <div className="cell-sub">{s.id.slice(0, 8)}</div>
        </div>
      ),
    },
    { head: 'Started', left: true, cell: s => dateTime(s.start) },
    { head: 'Last response', left: true, cell: s => ago(s.last) },
    {
      head: 'Models',
      left: true,
      cell: s => {
        const names = (s.models ?? []).map(shortModel).join(', ');
        return (
          <span className="cell-models" title={names}>
            {names}
          </span>
        );
      },
    },
    { head: 'Responses', cell: s => count(s.messages) },
    { head: 'Tokens', cell: s => tok(s.tokens) },
    { head: 'Cost', cell: s => usd(s.cost_usd) },
  ];
  const capped = r.session_count > r.sessions.length;
  return (
    <Section title="Sessions" sub={capped ? `the ${r.sessions.length} most recent of ${count(r.session_count)}` : 'most recent first'}>
      <Table columns={columns} rows={r.sessions} rowKey={s => s.id} />
    </Section>
  );
}

function YesNo({ on }: { on: boolean }) {
  return (
    <span className={on ? 'yes' : 'no'}>
      <Icon name={on ? 'check' : 'minus'} />
      {on ? 'Set up' : 'Not set up'}
    </span>
  );
}

function SetupRow({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div className="setup-row">
      <dt>{k}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function AccountSection({ info }: { info?: AccountInfo }) {
  if (!info) {
    return (
      <Section title="Account">
        <div className="section-sub">
          No Claude login found in this config dir. That's expected with an API key, or before the first sign-in.
        </div>
      </Section>
    );
  }
  return (
    <Section title="Account" sub="from the profile Claude Code saved in this config dir">
      <div className="setup">
        <dl>
          {info.plan && <SetupRow k="Plan">{info.plan}</SetupRow>}
          {info.email && <SetupRow k="Email">{info.email}</SetupRow>}
          {info.name && <SetupRow k="Name">{info.name}</SetupRow>}
          {info.organization && <SetupRow k="Organization">{info.organization}</SetupRow>}
          {info.role && <SetupRow k="Role"><span className="capitalize">{info.role}</span></SetupRow>}
        </dl>
        <dl>
          <SetupRow k="Extra usage">{info.extra_usage ? 'On' : 'Off'}</SetupRow>
          {info.subscription_since && <SetupRow k="Subscribed since">{fullDay(info.subscription_since)}</SetupRow>}
          {info.account_created && <SetupRow k="Account created">{fullDay(info.account_created)}</SetupRow>}
          {info.profile_updated && <SetupRow k="Profile last updated">{ago(info.profile_updated)}</SetupRow>}
        </dl>
      </div>
    </Section>
  );
}

function SetupSection({ a }: { a: AccountDetail }) {
  const s = a.setup;
  const arg = /\s/.test(a.name) ? `"${a.name}"` : a.name;
  const block =
    a.block.limit_source === 'config'
      ? `${usd(a.block.limit_usd)}, from your config`
      : a.block.limit_source === 'peak'
        ? `${usd(a.block.limit_usd)}, your 30-day peak`
        : 'None yet';
  return (
    <Section title="Setup and plan checks">
      <div className="setup">
        <dl>
          <SetupRow k="Claude Code hooks">
            <YesNo on={s.hooks} />
          </SetupRow>
          <SetupRow k="Claude Code plugin">
            <YesNo on={s.plugin} />
          </SetupRow>
          <SetupRow k="Status line">
            <YesNo on={s.status_line} />
          </SetupRow>
          <SetupRow k="5-hour estimate compares to">{block}</SetupRow>
          <SetupRow k="Weekly limit">{a.week.limit_usd ? usd(a.week.limit_usd) : 'Not set'}</SetupRow>
          <SetupRow k="Log files read">{count(a.files)}</SetupRow>
        </dl>
        <dl>
          <SetupRow k="Plan limits from">{hasPlan(a.plan) ? planSource(a.plan.source) : 'No report yet'}</SetupRow>
          <SetupRow k="Last check">{hasTime(s.last_attempt) ? ago(s.last_attempt) : 'Never'}</SetupRow>
          <SetupRow k="Last successful check">{hasTime(s.last_ok) ? ago(s.last_ok) : 'Never'}</SetupRow>
          {hasTime(s.next_allowed) && new Date(s.next_allowed).getTime() > Date.now() && (
            <SetupRow k="Next check allowed">{dateTime(s.next_allowed)}</SetupRow>
          )}
          {s.last_error && <SetupRow k="Last problem">{s.last_error}</SetupRow>}
        </dl>
      </div>
      {s.settings_error && (
        <div className="note">
          <Icon name="alert" />
          <span>{s.settings_error}</span>
        </div>
      )}
      {!s.hooks && !s.plugin && (
        <div className="hint setup-hint">
          <Icon name="info" />
          <span>
            To refresh plan limits whenever this account is used, run <code>ccmeter hook install {arg}</code>.
          </span>
        </div>
      )}
    </Section>
  );
}

function NowSection({ a }: { a: AccountDetail }) {
  return (
    <section className="now">
      <div className="card">
        <div className="section-head">
          <h3>Plan limits</h3>
        </div>
        {hasPlan(a.plan) ? (
          <PlanBars plan={a.plan} block={a.block} />
        ) : (
          <div className="hint no-plan">
            <Icon name="info" />
            <span>
              No plan limit report yet. Press the refresh button at the top of the page to check now.
            </span>
          </div>
        )}
        {a.plan_error && (
          <div className="note plan-note">
            <Icon name="alert" />
            <span>Plan limits: {a.plan_error}</span>
          </div>
        )}
      </div>
      <div className="card">
        <div className="section-head">
          <h3>Current 5-hour window</h3>
          <span className="section-sub">estimated from local logs</span>
        </div>
        <BlockEstimate block={a.block} />
      </div>
    </section>
  );
}

function setParam(url: URL, key: string, value: string | null) {
  const next = new URL(url);
  if (value === null) next.searchParams.delete(key);
  else next.searchParams.set(key, value);
  navigate(next.pathname + next.search, { replace: true, keepScroll: true });
}

export function Account({ name, tick, onEdit }: { name: string; tick: number; onEdit?: (name: string) => void }) {
  useTitle(`${name} · Claude Code usage`);
  const url = useUrl();
  const days = url.searchParams.get('days') === '7' ? 7 : 30;
  const metricParam = url.searchParams.get('metric');
  const metricKey: MetricKey = metricParam === 'tokens' || metricParam === 'responses' ? metricParam : 'cost';

  const { data: a, error, status, stale } = usePoll<AccountDetail>(`/api/accounts/${encodeURIComponent(name)}?days=${days}`, tick);

  if (status === 404) return <NotFound what={`There's no account named ${name}.`} />;
  if (!a) {
    return error ? (
      <div className="empty">
        <div className="missing">
          <Icon name="alert" />
          {error}
        </div>
      </div>
    ) : null;
  }

  const r = a.range;
  return (
    <>
      <div className="acct-head">
        <div className="card-head">
          <h2>{a.name}</h2>
          <PlanTag info={a.account} />
          <span className="status">{a.last_activity ? 'active ' + ago(a.last_activity) : 'no recent activity'}</span>
          {onEdit && (
            <button className="btn edit" type="button" onClick={() => onEdit(a.name)}>
              <Icon name="pencil" />
              Edit
            </button>
          )}
        </div>
        <EmailLine info={a.account} />
        <DirLine dir={a.dir} />
      </div>

      {!a.found ? (
        <>
          <div className="card missing-card">
            <div className="note">
              <Icon name="alert" />
              <span>
                No <code>projects/</code> folder found in this config dir, so there's no usage to show. It appears once Claude
                Code has run with <code>CLAUDE_CONFIG_DIR</code> set to this folder.
              </span>
            </div>
          </div>
          <AccountSection info={a.account} />
          <SetupSection a={a} />
        </>
      ) : (
        <>
          <section className="totals">
            <Tile label="Today" t={a.today} />
            <Tile label="Last 7 days" t={a.week} note={a.week.limit_usd ? `${Math.round(a.week.pct ?? 0)}% of limit` : undefined} />
            <Tile label="Last 30 days" t={a.month} />
          </section>

          <div className="filters">
            <Segmented
              label="Range"
              value={days}
              options={[
                { value: 7, label: '7 days' },
                { value: 30, label: '30 days' },
              ]}
              onChange={d => setParam(url, 'days', d === 30 ? null : String(d))}
            />
            <Segmented
              label="Measure"
              value={metricKey}
              options={(Object.keys(METRICS) as MetricKey[]).map(k => ({ value: k, label: METRICS[k].label }))}
              onChange={k => setParam(url, 'metric', k === 'cost' ? null : k)}
            />
            <span className="filter-note">
              {shortDate(r.from)} – {shortDate(r.to)}, whole days
            </span>
          </div>

          <div className={stale ? 'ranged dim' : 'ranged'}>
            <RangeStats r={r} />
            <DailySection r={r} metricKey={metricKey} />
            <div className="two">
              <ActivitySection r={r} metricKey={metricKey} />
              <TokenMix r={r} />
            </div>
            <NowSection a={a} />
            <WindowsSection r={r} metricKey={metricKey} />
            {r.messages > 0 && (
              <>
                <ModelsSection r={r} />
                <ProjectsSection r={r} />
                <SessionsSection r={r} />
              </>
            )}
            {!!a.unpriced?.length && (
              <div className="note">
                <Icon name="alert" />
                <span>
                  Unpriced models: {a.unpriced.join(', ')}. They count as $0 here. Add them under <code>pricing</code> in the config.
                </span>
              </div>
            )}
          </div>
          <AccountSection info={a.account} />
          <SetupSection a={a} />
        </>
      )}
      <footer>
        Costs are estimates at <Link href="/prices">API list prices</Link>, read from this config dir's logs. ccmeter looks back 30
        days.
      </footer>
    </>
  );
}
