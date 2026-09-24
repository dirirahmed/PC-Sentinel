import { useLayoutEffect, useMemo, useRef, useState, type PointerEvent } from "react";
import { axisTimeFormat, linearScale, linePath, nearestIndex, niceBytesMax, niceMax, ticks } from "../lib/chart";

export interface Series {
  key: string;
  label: string;
  /** CSS color, normally a var(--series-*) token. */
  color: string;
  values: (number | null)[];
}

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setWidth(el.clientWidth);
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

export function Sparkline({ values, max, color, label }: { values: (number | null)[]; max?: number; color: string; label: string }) {
  const top = max ?? niceMax(Math.max(0, ...values.map((v) => v ?? 0)));
  const pts = values.map((y, x) => ({ x, y }));
  const sx = linearScale([0, Math.max(values.length - 1, 1)], [0, 100]);
  const sy = linearScale([0, top], [29, 1]);
  const line = linePath(pts, sx, sy);
  const lastValid = pts.filter((p) => p.y != null);
  const area =
    line && lastValid.length > 1
      ? `${line}L${sx(lastValid[lastValid.length - 1].x).toFixed(1)},30L${sx(lastValid[0].x).toFixed(1)},30Z`
      : "";
  return (
    <svg className="sparkline" viewBox="0 0 100 30" preserveAspectRatio="none" role="img" aria-label={label}>
      {area && <path d={area} fill={color} opacity="0.12" />}
      <path d={line} fill="none" stroke={color} strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  );
}

const M = { top: 10, right: 12, bottom: 24, left: 52 };

export function TimeSeriesChart({
  times,
  series,
  format,
  axisFormat = format,
  yMax,
  height = 200,
  maxGapMs,
  showLegend = series.length > 1,
  emptyText = "No data for this range",
  bytes = false,
}: {
  times: number[];
  series: Series[];
  format: (v: number | null) => string;
  axisFormat?: (v: number) => string;
  /** Fixed axis maximum (e.g. 100 for percentages); otherwise derived from data. */
  yMax?: number;
  height?: number;
  showLegend?: boolean;
  /** Shown when samples exist but none carry this metric (e.g. no sensor). */
  emptyText?: string;
  /** Values are byte quantities; round the axis in binary units. */
  bytes?: boolean;
  /** Break the line when samples are further apart than this (agent was off). */
  maxGapMs?: number;
}) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);

  const top = useMemo(() => {
    if (yMax != null) return yMax;
    let m = 0;
    for (const s of series) for (const v of s.values) if (v != null && v > m) m = v;
    return bytes ? niceBytesMax(m) : niceMax(m);
  }, [series, yMax, bytes]);

  const innerW = Math.max(width - M.left - M.right, 10);
  const innerH = height - M.top - M.bottom;
  const t0 = times[0] ?? 0;
  const t1 = times[times.length - 1] ?? 1;
  const sx = linearScale([t0, t1 === t0 ? t0 + 1 : t1], [M.left, M.left + innerW]);
  const sy = linearScale([0, top], [M.top + innerH, M.top]);

  const xTicks = useMemo(() => {
    const n = Math.max(2, Math.min(6, Math.floor(innerW / 110)));
    return Array.from({ length: n }, (_, i) => t0 + ((t1 - t0) * i) / (n - 1));
  }, [t0, t1, innerW]);

  const hasData = times.length > 1 && series.some((s) => s.values.some((v) => v != null));
  const timeFormat = axisTimeFormat(t1 - t0);

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    if (x < M.left || x > M.left + innerW) return setHover(null);
    const target = t0 + ((x - M.left) / innerW) * (t1 - t0);
    setHover(nearestIndex(times, target));
  };

  const hoverX = hover != null ? sx(times[hover]) : 0;
  const tooltipLeft = hoverX > width * 0.6;

  return (
    <div className="chart" ref={ref}>
      {showLegend && (
        <ul className="legend">
          {series.map((s) => (
            <li key={s.key}>
              <span className="swatch" style={{ background: s.color }} />
              {s.label}
            </li>
          ))}
        </ul>
      )}
      <div className="chart-plot" style={{ height }}>
        {width > 0 && (
          <svg
            width={width}
            height={height}
            onPointerMove={onMove}
            onPointerLeave={() => setHover(null)}
            role="img"
            aria-label={`${series.map((s) => s.label).join(", ")} chart`}
          >
            {hasData &&
              ticks(top).map((v) => (
                <g key={v}>
                  <line className="grid" x1={M.left} x2={M.left + innerW} y1={sy(v)} y2={sy(v)} />
                  <text className="axis" x={M.left - 8} y={sy(v)} dy="0.32em" textAnchor="end">
                    {axisFormat(v)}
                  </text>
                </g>
              ))}
            {hasData &&
              xTicks.map((t, i) => (
                <text
                  key={t}
                  className="axis"
                  x={sx(t)}
                  y={height - 6}
                  textAnchor={i === 0 ? "start" : i === xTicks.length - 1 ? "end" : "middle"}
                >
                  {new Date(t).toLocaleString(undefined, timeFormat)}
                </text>
              ))}
            {series.map((s) => (
              <path
                key={s.key}
                d={linePath(
                  times.map((x, i) => ({ x, y: s.values[i] })),
                  sx,
                  sy,
                  maxGapMs,
                )}
                fill="none"
                stroke={s.color}
                strokeWidth="2"
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            ))}
            {hover != null && (
              <g className="crosshair">
                <line x1={hoverX} x2={hoverX} y1={M.top} y2={M.top + innerH} />
                {series.map((s) =>
                  s.values[hover] != null ? (
                    <circle key={s.key} cx={hoverX} cy={sy(s.values[hover] as number)} r="4" fill={s.color} />
                  ) : null,
                )}
              </g>
            )}
          </svg>
        )}
        {!hasData && <div className="chart-empty">{times.length > 1 ? emptyText : "Collecting data…"}</div>}
        {hover != null && hasData && (
          <div className="tooltip" style={tooltipLeft ? { right: width - hoverX + 12 } : { left: hoverX + 12 }}>
            <div className="tooltip-time">{new Date(times[hover]).toLocaleString()}</div>
            {series.map((s) => (
              <div key={s.key} className="tooltip-row">
                <span className="swatch" style={{ background: s.color }} />
                <span>{s.label}</span>
                <strong>{format(s.values[hover])}</strong>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
