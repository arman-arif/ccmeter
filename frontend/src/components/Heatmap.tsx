import { useState, type KeyboardEvent } from 'react';
import { useWidth } from '../hooks';
import { ChartTip } from './BarChart';

export const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
export const hourLabel = (h: number) => String(h).padStart(2, '0') + ':00';

export interface HeatCell {
  weekday: number;
  hour: number;
  value: number;
  detail: string;
}

const GUTTER = 34;
const CELL_H = 18;
const AXIS_H = 18;
const BINS = 5;

// Weekday by hour, in one hue from light to dark. Empty hours stay neutral.
export function Heatmap({ cells, label, format }: { cells: HeatCell[]; label: string; format: (v: number) => string }) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [active, setActive] = useState<[number, number] | null>(null);

  const byKey = new Map(cells.map(c => [`${c.weekday}:${c.hour}`, c]));
  const max = Math.max(0, ...cells.map(c => c.value));
  const bin = (v: number) => (v <= 0 || max <= 0 ? 0 : Math.max(1, Math.ceil((v / max) * BINS)));

  const slotW = Math.max((width - GUTTER) / 24, 0);
  const slotH = CELL_H + 2;
  const height = slotH * 7 + AXIS_H;
  const x = (hour: number) => GUTTER + hour * slotW;
  const y = (day: number) => day * slotH;

  const onKey = (e: KeyboardEvent) => {
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [0, -1],
      ArrowRight: [0, 1],
      ArrowUp: [-1, 0],
      ArrowDown: [1, 0],
    };
    const m = moves[e.key];
    if (!m) return;
    e.preventDefault();
    setActive(a => {
      const [d, h] = a ?? [0, 0];
      return [Math.min(Math.max(d + m[0], 0), 6), Math.min(Math.max(h + m[1], 0), 23)];
    });
  };

  const shownCell = active && byKey.get(`${active[0]}:${active[1]}`);

  return (
    <>
      <div className="chart" ref={ref} style={{ height }}>
        {width > 0 && (
          <svg
            height={height}
            role="img"
            aria-label={`${label}. Use the arrow keys to read each hour.`}
            tabIndex={0}
            onKeyDown={onKey}
            onFocus={() => setActive(a => a ?? [0, 0])}
            onBlur={() => setActive(null)}
            onPointerLeave={() => setActive(null)}
          >
            {WEEKDAYS.map((name, d) => (
              <text key={name} className="axis" x={0} y={y(d) + CELL_H - 5}>
                {name}
              </text>
            ))}
            {[0, 6, 12, 18].map(h => (
              <text key={h} className="axis" x={x(h)} y={height - 4}>
                {hourLabel(h)}
              </text>
            ))}
            {WEEKDAYS.map((_, d) =>
              Array.from({ length: 24 }, (_, h) => {
                const c = byKey.get(`${d}:${h}`);
                const on = active?.[0] === d && active?.[1] === h;
                return (
                  <g key={h}>
                    <rect
                      className={`heat heat-${bin(c?.value ?? 0)}${on ? ' on' : ''}`}
                      x={x(h)}
                      y={y(d)}
                      width={Math.max(slotW - 2, 1)}
                      height={CELL_H}
                      rx={3}
                    />
                    <rect className="hit" x={x(h)} y={y(d)} width={slotW} height={slotH} onPointerEnter={() => setActive([d, h])} />
                  </g>
                );
              }),
            )}
          </svg>
        )}
        {active && (
          <ChartTip x={x(active[1]) + slotW / 2} y={y(active[0]) - 4} width={width}>
            <div className="muted">
              {WEEKDAYS[active[0]]} {hourLabel(active[1])}
            </div>
            {shownCell ? (
              <>
                <b>{format(shownCell.value)}</b> <span className="muted">{shownCell.detail}</span>
              </>
            ) : (
              <span className="muted">No usage</span>
            )}
          </ChartTip>
        )}
      </div>
      <div className="heat-legend">
        <span>Less</span>
        {[1, 2, 3, 4, 5].map(b => (
          <svg key={b} width="14" height="14" aria-hidden="true">
            <rect className={`heat heat-${b}`} width="14" height="14" rx="3" />
          </svg>
        ))}
        <span>More, up to {format(max)} in one hour of the week</span>
      </div>
    </>
  );
}
