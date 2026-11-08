import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, type AccountsResponse, type AccountView, type SaveAccount } from '../api';
import { usd } from '../format';
import { Icon } from './Icon';

interface FormState {
  original: string;
  name: string;
  dir: string;
  block: string;
  weekly: string;
}

const blankForm: FormState = { original: '', name: '', dir: '', block: '', weekly: '' };

const formFor = (a: AccountView): FormState => ({
  original: a.name,
  name: a.name,
  dir: a.dir,
  block: a.block_limit_usd ? String(a.block_limit_usd) : '',
  weekly: a.weekly_limit_usd ? String(a.weekly_limit_usd) : '',
});

// The Accounts panel. It starts with the form collapsed, except when opened to
// edit an account or when there are no accounts yet.
export function AccountsDialog(props: {
  accounts: AccountsResponse;
  editName?: string;
  onClose: () => void;
  onChange: (next: AccountsResponse) => void;
  onSaved: (original: string, name: string) => void;
  onRemoved: (name: string) => void;
}) {
  const { accounts: state, editName, onClose, onChange, onSaved, onRemoved } = props;
  const list = state.accounts ?? [];
  const editing = list.find(a => a.name === editName);

  const ref = useRef<HTMLDialogElement>(null);
  const [form, setForm] = useState<FormState>(editing ? formFor(editing) : blankForm);
  const [open, setOpen] = useState(state.editable && (!!editing || list.length === 0));
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);
  const nameInput = useRef<HTMLInputElement>(null);
  const dirInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const dlg = ref.current;
    if (dlg && !dlg.open) dlg.showModal();
  }, []);

  useEffect(() => {
    if (open) (form.original ? dirInput : nameInput).current?.focus();
    // Only when the form opens, not on every keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const set = (k: keyof FormState) => (e: { target: { value: string } }) => setForm(f => ({ ...f, [k]: e.target.value }));

  const startEdit = (a: AccountView) => {
    setForm(formFor(a));
    setError('');
    setOpen(true);
    setTimeout(() => dirInput.current?.focus());
  };

  const closeForm = () => {
    setForm(blankForm);
    setError('');
    setOpen(false);
  };

  const toggleAdd = () => {
    if (open) return closeForm();
    setForm(blankForm);
    setError('');
    setOpen(true);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const num = (v: string) => (v === '' ? 0 : Number(v));
    const body: SaveAccount = {
      original: form.original,
      name: form.name.trim(),
      dir: form.dir.trim(),
      block_limit_usd: num(form.block),
      weekly_limit_usd: num(form.weekly),
    };
    setSaving(true);
    try {
      onChange(await api<AccountsResponse>('POST', '/api/accounts', body));
      onSaved(body.original, body.name);
      closeForm();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async (name: string) => {
    try {
      onChange(await api<AccountsResponse>('DELETE', '/api/accounts?name=' + encodeURIComponent(name)));
      if (form.original === name) closeForm();
      onRemoved(name);
    } catch (err) {
      setError((err as Error).message);
    }
    setConfirming(null);
  };

  // Archiving keeps the account, its limits and its history; it only takes it
  // out of the meter until it is restored.
  const setArchived = async (a: AccountView, archived: boolean) => {
    setError('');
    try {
      onChange(await api<AccountsResponse>('POST', '/api/accounts/archive', { name: a.name, archived }));
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <dialog
      className="panel"
      ref={ref}
      aria-labelledby="acct-title"
      onClose={onClose}
      onClick={e => {
        if (e.target === ref.current) ref.current?.close();
      }}
    >
      <div className="panel-head">
        <Icon name="users" />
        <h3 id="acct-title">Accounts</h3>
        <div className="spacer" />
        <button className="icon-btn" type="button" aria-label="Close" onClick={() => ref.current?.close()}>
          <Icon name="x" />
        </button>
      </div>
      <div className="panel-body">
        <div className="cfg-path">
          {state.from_file ? 'Saved in ' : 'Not saved yet. Changes will be written to '}
          {state.config_path}
        </div>
        {list.length === 0 && <div className="cfg-path">No accounts configured.</div>}
        {list.map(a => (
          <div className={a.archived ? 'acct-row archived' : 'acct-row'} key={a.name}>
            <div className="acct-main">
              <div className="acct-name">
                {a.name}
                {a.archived && <span className="tag">Archived</span>}
              </div>
              <div className="acct-dir" title={a.dir}>
                {a.dir}
              </div>
              <div className="acct-meta">
                {!!a.block_limit_usd && <span>5h limit {usd(a.block_limit_usd)}</span>}
                {!!a.weekly_limit_usd && <span>weekly limit {usd(a.weekly_limit_usd)}</span>}
                {!a.has_projects && (
                  <span className="warn">
                    <Icon name="alert" />
                    no projects/ folder
                  </span>
                )}
              </div>
            </div>
            {state.editable &&
              (confirming === a.name ? (
                <span className="confirm">
                  Remove?
                  <button className="btn danger" type="button" onClick={() => remove(a.name)}>
                    Remove
                  </button>
                  <button className="btn" type="button" onClick={() => setConfirming(null)}>
                    Keep
                  </button>
                </span>
              ) : (
                <span className="actions">
                  <button className="icon-btn" type="button" aria-label={`Edit ${a.name}`} title="Edit" onClick={() => startEdit(a)}>
                    <Icon name="pencil" />
                  </button>
                  <button
                    className="icon-btn"
                    type="button"
                    aria-label={a.archived ? `Restore ${a.name}` : `Archive ${a.name}`}
                    title={a.archived ? 'Restore' : 'Archive, keeping its history'}
                    onClick={() => setArchived(a, !a.archived)}
                  >
                    <Icon name={a.archived ? 'unarchive' : 'archive'} />
                  </button>
                  <button className="icon-btn danger" type="button" aria-label={`Remove ${a.name}`} title="Remove" onClick={() => setConfirming(a.name)}>
                    <Icon name="trash" />
                  </button>
                </span>
              ))}
          </div>
        ))}

        {state.editable && (
          <button className="link-btn add-toggle" type="button" aria-expanded={open} aria-controls="acct-form" onClick={toggleAdd}>
            <Icon name="plus" />
            Add account
            <Icon name="chevron" className="chev" />
          </button>
        )}

        {state.editable && open && (
          <form className="acct-form" id="acct-form" autoComplete="off" onSubmit={submit}>
            <div className="form-title">
              <Icon name={form.original ? 'pencil' : 'plus'} />
              {form.original ? `Edit ${form.original}` : 'Add account'}
            </div>
            <div className="field">
              <label htmlFor="f-name">Name</label>
              <input id="f-name" ref={nameInput} required maxLength={64} placeholder="work" value={form.name} onChange={set('name')} />
            </div>
            <div className="field">
              <label htmlFor="f-dir">Config directory</label>
              <input id="f-dir" ref={dirInput} required placeholder="/mnt/data/claude-work or ~/.claude-work" spellCheck={false} value={form.dir} onChange={set('dir')} />
              <span className="hint">
                The folder that contains <code>projects/</code>, the same path you pass as CLAUDE_CONFIG_DIR. It can be anywhere.
              </span>
            </div>
            <div className="row2">
              <div className="field">
                <label htmlFor="f-block">5-hour limit (USD, optional)</label>
                <input id="f-block" type="number" min="0" step="0.01" placeholder="your 30-day peak" value={form.block} onChange={set('block')} />
              </div>
              <div className="field">
                <label htmlFor="f-weekly">Weekly limit (USD, optional)</label>
                <input id="f-weekly" type="number" min="0" step="0.01" placeholder="none" value={form.weekly} onChange={set('weekly')} />
              </div>
            </div>
            <div className="form-actions">
              {error && (
                <span className="form-error">
                  <Icon name="alert" />
                  <span>{error}</span>
                </span>
              )}
              <button className="btn" type="button" onClick={closeForm}>
                Cancel
              </button>
              <button className="btn primary" type="submit" disabled={saving}>
                {form.original ? 'Save changes' : 'Add account'}
              </button>
            </div>
          </form>
        )}

        {error && !open && (
          <div className="note">
            <Icon name="alert" />
            <span>{error}</span>
          </div>
        )}

        {!state.editable && (
          <div className="readonly-note">
            <Icon name="lock" />
            <span>Editing is turned off here. Use ccmeter add / remove on the machine running the dashboard.</span>
          </div>
        )}
      </div>
    </dialog>
  );
}
