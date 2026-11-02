import { useState, type ReactNode } from 'react';
import { Icon } from './Icon';

export interface Column<T> {
  head: string;
  cell: (row: T) => ReactNode;
  left?: boolean; // text columns; numbers align right
}

// Table shows the first rows and offers the rest, so long lists don't bury the page.
export function Table<T>(props: { columns: Column<T>[]; rows: T[]; rowKey: (row: T) => string; limit?: number }) {
  const { columns, rows, rowKey, limit = 10 } = props;
  const [all, setAll] = useState(false);
  const shown = all ? rows : rows.slice(0, limit);
  return (
    <>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {columns.map(c => (
                <th key={c.head} className={c.left ? 'left' : undefined}>
                  {c.head}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {shown.map(r => (
              <tr key={rowKey(r)}>
                {columns.map(c => (
                  <td key={c.head} className={c.left ? 'left' : undefined}>
                    {c.cell(r)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {rows.length > limit && (
        <button className="link-btn more" type="button" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${rows.length}`}
        </button>
      )}
    </>
  );
}

// TableView is the table twin of a chart: every value can be read without hovering.
export function TableView({ title, children }: { title: string; children: ReactNode }) {
  return (
    <details>
      <summary>
        <Icon name="chevron" />
        {title}
      </summary>
      {children}
    </details>
  );
}

// Segmented is one choice from a few, in the filter row.
export function Segmented<V extends string | number>(props: {
  label: string;
  value: V;
  options: { value: V; label: string }[];
  onChange: (v: V) => void;
}) {
  return (
    <div className="seg" role="group" aria-label={props.label}>
      {props.options.map(o => (
        <button key={String(o.value)} type="button" aria-pressed={o.value === props.value} onClick={() => props.onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}
