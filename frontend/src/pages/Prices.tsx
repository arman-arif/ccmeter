import type { ListedPrice, PricesReport, PriceView, UsedPrice } from '../api';
import { Icon } from '../components/Icon';
import { Section } from '../components/Stats';
import { Table, type Column } from '../components/Table';
import { count, fullDate, rate, shortModel, tok, usd } from '../format';
import { usePoll, useTitle } from '../hooks';

// One column per kind of token, in the order Anthropic's price list uses.
function priceColumns<T>(get: (row: T) => PriceView | undefined): Column<T>[] {
  const col = (head: string, value: (p: PriceView) => number): Column<T> => ({
    head,
    cell: row => {
      const p = get(row);
      return p ? rate(value(p)) : '';
    },
  });
  return [
    col('Input', p => p.input),
    col('Cache write 5m', p => p.cache_write),
    col('Cache write 1h', p => p.cache_write_1h),
    col('Cache read', p => p.cache_read),
    col('Output', p => p.output),
  ];
}

function PricedAs({ u }: { u: UsedPrice }) {
  if (u.source === 'override') {
    return (
      <>
        Your override <code>{u.match}</code>
      </>
    );
  }
  if (u.source === 'builtin') return <>Built-in: {u.match}</>;
  return (
    <span className="missing-price">
      <Icon name="alert" />
      No price, counts as $0
    </span>
  );
}

function UsedSection({ used }: { used: UsedPrice[] }) {
  const columns: Column<UsedPrice>[] = [
    { head: 'Model', left: true, cell: u => <span title={u.model}>{shortModel(u.model)}</span> },
    { head: 'Priced as', left: true, cell: u => <PricedAs u={u} /> },
    ...priceColumns<UsedPrice>(u => u.price),
    { head: 'Accounts', left: true, cell: u => u.accounts.join(', ') },
    { head: 'Responses', cell: u => count(u.messages) },
    { head: 'Tokens', cell: u => tok(u.tokens) },
    { head: 'Cost', cell: u => (u.price ? usd(u.cost_usd) : '') },
  ];
  return (
    <Section title="Models you've used" sub="last 30 days, all accounts">
      {used.length ? (
        <Table columns={columns} rows={used} rowKey={u => u.model} limit={25} />
      ) : (
        <div className="section-sub">No models in the logs yet.</div>
      )}
    </Section>
  );
}

function OverridesSection({ report }: { report: PricesReport }) {
  const columns: Column<ListedPrice>[] = [
    { head: 'Matches names containing', left: true, cell: o => <code>{o.name}</code> },
    ...priceColumns<ListedPrice>(o => o.price),
  ];
  const sample = '"pricing": {\n  "model-name": { "input": 3, "output": 15 }\n}';
  return (
    <Section title="Your overrides" sub="these win over the built-in prices">
      {report.overrides.length > 0 && <Table columns={columns} rows={report.overrides} rowKey={o => o.name} limit={25} />}
      <div className={report.overrides.length > 0 ? 'section-foot section-sub' : 'section-sub'}>
        {report.overrides.length === 0 && 'You have no overrides. '}
        To set a price, add it under <code>pricing</code> in <code>{report.config_path}</code>. ccmeter picks it up on its next
        rescan, within 10 seconds by default. Cache prices you leave out default to 1.25x input (5-minute write), 2x input (1-hour write) and 0.1x input (read).
      </div>
      {report.overrides.length === 0 && (
        <pre className="config-sample">
          <code>{sample}</code>
        </pre>
      )}
    </Section>
  );
}

function BuiltinSection({ report }: { report: PricesReport }) {
  const columns: Column<ListedPrice>[] = [
    { head: 'Model', left: true, cell: b => b.name },
    ...priceColumns<ListedPrice>(b => b.price),
  ];
  return (
    <Section title="Built-in prices">
      <Table columns={columns} rows={report.builtin} rowKey={b => b.name} limit={25} />
      <div className="section-foot section-sub">
        Checked against{' '}
        <a href={report.source_url} target="_blank" rel="noreferrer">
          Anthropic's pricing page
        </a>{' '}
        on {fullDate(report.checked_on)}, and they only change with a new version of ccmeter. A model newer than this list takes its
        family's newest price. Not counted: fast mode, batch discounts and the US-only inference premium.
      </div>
    </Section>
  );
}

export function Prices({ tick }: { tick: number }) {
  useTitle('Prices · ccmeter');
  const { data: report, error } = usePoll<PricesReport>('/api/prices', tick);
  if (!report) {
    return error ? (
      <div className="empty">
        <div className="missing">
          <Icon name="alert" />
          {error}
        </div>
      </div>
    ) : null;
  }
  const unpriced = report.used.filter(u => !u.price).map(u => u.model);
  return (
    <>
      <div className="page-intro">
        <h2>Prices</h2>
        <p>The prices ccmeter uses to estimate cost, in USD per million tokens. On a subscription they measure usage: they aren't what you're charged.</p>
      </div>
      {unpriced.length > 0 && (
        <div className="note page-note">
          <Icon name="alert" />
          <span>
            {unpriced.join(', ')} {unpriced.length === 1 ? 'has' : 'have'} no price, so the cost totals leave {unpriced.length === 1 ? 'it' : 'them'} out. Add a
            price under Your overrides below.
          </span>
        </div>
      )}
      <UsedSection used={report.used} />
      <OverridesSection report={report} />
      <BuiltinSection report={report} />
    </>
  );
}
