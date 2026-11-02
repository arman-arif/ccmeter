import { useCallback, useEffect, useState } from 'react';
import { api, type AccountsResponse, type Report } from './api';
import { AccountsDialog } from './components/AccountsDialog';
import { Icon } from './components/Icon';
import { usePoll } from './hooks';
import { Link, matchRoute, useUrl } from './router';
import { Overview } from './pages/Overview';
import { NotFound } from './pages/NotFound';

type Theme = 'light' | 'dark';

function currentTheme(): Theme {
  const set = document.documentElement.dataset.theme;
  if (set === 'light' || set === 'dark') return set;
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function useTheme() {
  const [theme, setTheme] = useState<Theme>(currentTheme);
  const toggle = () => {
    const next = theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    try {
      localStorage.setItem('ccmeter-theme', next);
    } catch {
      // Private mode: the toggle still works until the page closes.
    }
    setTheme(next);
  };
  return [theme, toggle] as const;
}

const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));

export function App() {
  const url = useUrl();
  const route = matchRoute(url.pathname);
  const [theme, toggleTheme] = useTheme();

  // Bumping tick reloads whatever the current page is showing.
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick(t => t + 1), []);
  const summary = usePoll<Report>('/api/summary', tick);

  const [accounts, setAccounts] = useState<AccountsResponse>({ config_path: '', from_file: false, editable: false, accounts: [] });
  const loadAccounts = useCallback(async () => {
    try {
      const next = await api<AccountsResponse>('GET', '/api/accounts');
      setAccounts(next);
      return next;
    } catch {
      return null;
    }
  }, []);
  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  const [dialog, setDialog] = useState<{ edit?: string } | null>(null);
  const openAccounts = async (edit?: string) => {
    await loadAccounts();
    setDialog({ edit });
  };

  const [checking, setChecking] = useState(false);
  const refresh = async () => {
    reload();
    loadAccounts();
    if (checking) return;
    // Also ask for fresh plan limits, then reload once the checks have had time to run.
    try {
      await api('POST', '/api/limits/refresh', {});
    } catch {
      return; // Not allowed from this machine: the reload above is all we can do.
    }
    setChecking(true);
    for (let tries = 0; tries < 30; tries++) {
      await sleep(1500);
      const st = await api<{ refreshing?: boolean }>('GET', '/api/limits/status').catch(() => ({ refreshing: false }));
      if (!st.refreshing) break;
    }
    setChecking(false);
    reload();
  };

  const onEdit = accounts.editable ? (name: string) => openAccounts(name) : undefined;

  return (
    <div className="wrap">
      <header>
        <h1>
          <Link className="brand" href="/">
            <Icon name="gauge" />
            Claude Code usage
          </Link>
        </h1>
        <div className="spacer" />
        <span className="status">
          {summary.error ? (
            <>
              <Icon name="alert" />
              <span>{summary.error}</span>
            </>
          ) : (
            <>
              <Icon name="refresh" />
              <span>
                {checking
                  ? 'checking plan limits'
                  : summary.data
                    ? 'updated ' + new Date(summary.data.generated_at).toLocaleTimeString()
                    : 'loading'}
              </span>
            </>
          )}
        </span>
        <button className="btn" type="button" aria-label="Refresh now" onClick={refresh}>
          <Icon name="refresh" />
        </button>
        <button className="btn" type="button" onClick={() => openAccounts()}>
          <Icon name="users" />
          Accounts
        </button>
        <button className="btn" type="button" aria-label="Toggle theme" onClick={toggleTheme}>
          <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
        </button>
      </header>

      {route.page === 'overview' && <Overview report={summary.data} onEdit={onEdit} onAdd={() => openAccounts()} />}
      {route.page !== 'overview' && <NotFound what="That page doesn't exist." />}

      {dialog && (
        <AccountsDialog
          accounts={accounts}
          editName={dialog.edit}
          onClose={() => setDialog(null)}
          onChange={next => {
            setAccounts(next);
            reload();
          }}
          onSaved={() => {}}
          onRemoved={() => {}}
        />
      )}
    </div>
  );
}
