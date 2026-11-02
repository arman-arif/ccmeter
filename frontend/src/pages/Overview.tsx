import type { Report, Summary } from '../api';
import { BarChart } from '../components/BarChart';
import { DirLine, PeriodStat, Tile } from '../components/Stats';
import { Icon } from '../components/Icon';
import { BlockEstimate, hasPlan, PlanBars } from '../components/Limits';
import { ago, longDate, shortDate, tok, usd } from '../format';
import { useTitle } from '../hooks';
import { accountPath, Link } from '../router';

function EditButton({ name, onEdit }: { name: string; onEdit?: (name: string) => void }) {
  if (!onEdit) return null;
  return (
    <button className="icon-btn edit" type="button" aria-label={`Edit ${name}`} title="Edit account" onClick={() => onEdit(name)}>
      <Icon name="pencil" />
    </button>
  );
}

function AccountCard({ a, onEdit }: { a: Summary; onEdit?: (name: string) => void }) {
  const title = (
    <h2>
      <Link href={accountPath(a.name)}>{a.name}</Link>
    </h2>
  );
  const details = (
    <Link className="card-link" href={accountPath(a.name)}>
      View details
      <Icon name="chevron" />
    </Link>
  );
  if (!a.found) {
    return (
      <article className="card">
        <div className="card-head">
          {title}
          <EditButton name={a.name} onEdit={onEdit} />
        </div>
        <DirLine dir={a.dir} />
        <div className="note">
          <Icon name="alert" />
          <span>
            No <code>projects/</code> folder found in this config dir.
          </span>
        </div>
        {details}
      </article>
    );
  }
  const daily = a.daily ?? [];
  return (
    <article className="card">
      <div className="card-head">
        {title}
        <span className="status">{a.last_activity ? 'active ' + ago(a.last_activity) : 'no recent activity'}</span>
        <EditButton name={a.name} onEdit={onEdit} />
      </div>
      <DirLine dir={a.dir} />
      {hasPlan(a.plan) ? (
        <PlanBars plan={a.plan} block={a.block} />
      ) : (
        <>
          <BlockEstimate block={a.block} />
          <div className="hint">
            <Icon name="info" />
            <span>
              Estimated from local logs. For your real 5-hour and weekly plan limits, run <code>ccmeter hook install</code>.
            </span>
          </div>
        </>
      )}
      {a.plan_error && (
        <div className="note plan-note">
          <Icon name="alert" />
          <span>Plan limits: {a.plan_error}</span>
        </div>
      )}
      <div className="periods">
        <PeriodStat label="Today" p={a.today} />
        <PeriodStat label="7 days" p={a.week} />
        <PeriodStat label="30 days" p={a.month} />
      </div>
      <div className="chart-title">Daily cost, last 30 days</div>
      <BarChart
        label="Daily cost, last 30 days"
        height={120}
        format={usd}
        firstLabel={daily.length ? shortDate(daily[0].date) : undefined}
        lastLabel={daily.length ? shortDate(daily[daily.length - 1].date) : undefined}
        bars={daily.map(d => ({
          key: d.date,
          value: d.cost_usd,
          title: longDate(d.date),
          detail: `· ${tok(d.tokens)} tokens`,
        }))}
      />
      {!!a.unpriced?.length && (
        <div className="note">
          <Icon name="alert" />
          <span>
            Unpriced models: {a.unpriced.join(', ')}. Add them under <code>pricing</code> in the config.
          </span>
        </div>
      )}
      {details}
    </article>
  );
}

export function Overview(props: { report?: Report; onEdit?: (name: string) => void; onAdd: () => void }) {
  const { report, onEdit, onAdd } = props;
  useTitle('Claude Code Usage Meter');
  if (!report) return null;
  const accounts = report.accounts ?? [];
  return (
    <>
      {accounts.length > 0 && (
        <section className="totals">
          <Tile label="Today, all accounts" t={report.total.today} />
          <Tile label="Last 7 days" t={report.total.week} />
          <Tile label="Last 30 days" t={report.total.month} />
        </section>
      )}
      <section className="grid">
        {accounts.map(a => (
          <AccountCard key={a.name} a={a} onEdit={onEdit} />
        ))}
        {accounts.length === 0 && (
          <div className="empty">
            No accounts yet.
            <br />
            <br />
            <button className="btn primary" type="button" onClick={onAdd}>
              <Icon name="plus" />
              Add an account
            </button>
          </div>
        )}
      </section>
      <footer>
        Costs are estimates at <Link href="/prices">API list prices</Link>, read from each config dir's <code>projects/*.jsonl</code> logs. The 5-hour
        percentage is against <code>block_limit_usd</code> when set, otherwise against your own busiest window in the last 30
        days.
      </footer>
    </>
  );
}
