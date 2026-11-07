import { useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { useWidth } from '../hooks';
import { niceMax } from '../format';

export interface Bar {
  key: string;
  value: number;
  title: string; // what this bar is, shown in the tooltip
  detail: string; // the other figures for this bar
}

const MAX_BAR = 24;
const PAD_LEFT = 44;
const PAD_TOP = 6;
const PAD_BOTTOM = 18;

// One series of columns: thin marks, 4px rounded tops, square at the baseline,
// 2px gaps, recessive grid. Hovering or using the arrow keys shows a tooltip.
export function BarChart(props: {
  bars: Bar[];
  label: string;
  format: (v: number) => string;
  firstLabel?: string;
  lastLabel?: string;
  height?: number;
  integer?: boolean; // whole numbers only, so no tick falls between two values
}) {
  const { bars, label, format, firstLabel, lastLabel, height = 140, integer } = props;
  const [ref, width] = useWidth<HTMLDivElement>();
  const [active, setActive] = useState<number | null>(null);
  const shown = active !== null && active < bars.length ? active : null;

  const plotW = Math.max(width - PAD_LEFT, 0);
  const plotH = height - PAD_TOP - PAD_BOTTOM;
  const baseline = PAD_TOP + plotH;
  const top = niceMax(Math.max(0, ...bars.map(b => b.value)));
  const slot = bars.length ? plotW / bars.length : 0;
  const barW = Math.max(Math.min(slot - 2, MAX_BAR), 1);
  const ticks = [1, 0.5].filter(f => !integer || Number.isInteger(top * f));
  const barTop = (b: Bar) => baseline - (plotH * b.value) / top;
  const center = (i: number) => PAD_LEFT + i * slot + slot / 2;

  const onKey = (e: KeyboardEvent) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const step = e.key === 'ArrowLeft' ? -1 : 1;
    setActive(i => Math.min(Math.max((i ?? bars.length - 1) + step, 0), bars.length - 1));
  };

  return (
    <div className="chart" ref={ref} style={{ height }}>
      {width > 0 && (
        <svg
          height={height}
          role="img"
          aria-label={`${label}. Use the left and right arrow keys to read each bar.`}
          tabIndex={0}
          onKeyDown={onKey}
          onFocus={() => setActive(i => i ?? bars.length - 1)}
          onBlur={() => setActive(null)}
          onPointerLeave={() => setActive(null)}
        >
          {ticks.map(f => {
            const y = PAD_TOP + plotH * (1 - f);
            return (
              <g key={f}>
                <line className="gridline" x1={PAD_LEFT} x2={width} y1={y} y2={y} />
                <text className="axis" x={PAD_LEFT - 6} y={y + 3} textAnchor="end">
                  {format(top * f)}
                </text>
              </g>
            );
          })}
          <line className="baseline" x1={PAD_LEFT} x2={width} y1={baseline} y2={baseline} />
          {bars.map((b, i) => {
            const y = barTop(b);
            const h = baseline - y;
            const x = center(i) - barW / 2;
            const r = Math.min(4, barW / 2, h);
            return (
              <g key={b.key}>
                {h > 0 && (
                  <path
                    className={i === shown ? 'bar-mark on' : 'bar-mark'}
                    d={`M${x},${baseline}V${y + r}Q${x},${y} ${x + r},${y}H${x + barW - r}Q${x + barW},${y} ${x + barW},${y + r}V${baseline}Z`}
                  />
                )}
                <rect
                  className="hit"
                  x={PAD_LEFT + i * slot}
                  y={PAD_TOP}
                  width={slot}
                  height={plotH + PAD_BOTTOM}
                  onPointerEnter={() => setActive(i)}
                />
              </g>
            );
          })}
          {firstLabel && (
            <text className="axis" x={PAD_LEFT} y={height - 4}>
              {firstLabel}
            </text>
          )}
          {lastLabel && (
            <text className="axis" x={width} y={height - 4} textAnchor="end">
              {lastLabel}
            </text>
          )}
        </svg>
      )}
      {shown !== null && (
        <ChartTip x={center(shown)} y={barTop(bars[shown]) - 6} width={width}>
          <div className="muted">{bars[shown].title}</div>
          <b>{format(bars[shown].value)}</b> <span className="muted">{bars[shown].detail}</span>
        </ChartTip>
      )}
    </div>
  );
}

// ChartTip floats above a point in its chart and stays inside the chart's width.
// It's placed straight in the DOM once measured, not through state: its width
// can depend on where it sits (Firefox snaps text to device pixels), so feeding
// the measurement back into a render could loop until React gives up.
export function ChartTip({ x, y, width, children }: { x: number; y: number; width: number; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const half = el.offsetWidth / 2;
    el.style.left = `${Math.max(half, Math.min(x, width - half))}px`;
  });
  return (
    <div className="tip" ref={ref} role="status" style={{ left: x, top: y }}>
      {children}
    </div>
  );
}
