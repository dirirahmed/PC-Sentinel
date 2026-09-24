export interface XY {
  x: number;
  y: number | null;
}

/** Rounds a data maximum up to a "nice" axis maximum (1, 2, 2.5, 5 x 10^n). */
export function niceMax(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 1;
  const exp = Math.floor(Math.log10(v));
  const base = 10 ** exp;
  for (const step of [1, 2, 2.5, 5, 10]) {
    if (v <= step * base) return step * base;
  }
  return 10 * base;
}

/** niceMax in binary units, so byte axes read 0 / 5 KB / 10 KB rather than 4.9 KB steps. */
export function niceBytesMax(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 1024;
  const unit = 1024 ** Math.max(0, Math.floor(Math.log(v) / Math.log(1024)));
  return niceMax(v / unit) * unit;
}

export function linearScale(domain: [number, number], range: [number, number]) {
  const [d0, d1] = domain;
  const [r0, r1] = range;
  const span = d1 - d0 || 1;
  return (v: number) => r0 + ((v - d0) / span) * (r1 - r0);
}

/**
 * Builds an SVG path, breaking the line wherever a value is missing (null)
 * or where consecutive points are further apart than maxGap, so outages in
 * collection show as gaps rather than misleading straight lines.
 */
export function linePath(points: XY[], sx: (v: number) => number, sy: (v: number) => number, maxGap = Infinity): string {
  let d = "";
  let penDown = false;
  let prevX = -Infinity;
  for (const p of points) {
    if (p.y == null || !Number.isFinite(p.y)) {
      penDown = false;
      continue;
    }
    if (p.x - prevX > maxGap) penDown = false;
    d += `${penDown ? "L" : "M"}${sx(p.x).toFixed(1)},${sy(p.y).toFixed(1)}`;
    penDown = true;
    prevX = p.x;
  }
  return d;
}

/**
 * Evenly spaced ticks from 0 to max inclusive. Axis maxima from niceMax that
 * start with 5 or 2.5 split into five steps so tick values stay round.
 */
export function ticks(max: number): number[] {
  const lead = max / 10 ** Math.floor(Math.log10(max || 1));
  const count = lead === 5 || lead === 2.5 ? 5 : 4;
  return Array.from({ length: count + 1 }, (_, i) => (max / count) * i);
}

/** Index of the point whose x is closest to target (points sorted by x). */
export function nearestIndex(xs: number[], target: number): number {
  if (xs.length === 0) return -1;
  let lo = 0;
  let hi = xs.length - 1;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (xs[mid] < target) lo = mid;
    else hi = mid;
  }
  return Math.abs(xs[lo] - target) <= Math.abs(xs[hi] - target) ? lo : hi;
}

/** Axis label precision follows the visible span so ticks are never identical. */
export function axisTimeFormat(spanMs: number): Intl.DateTimeFormatOptions {
  if (spanMs < 10 * 60_000) return { hour: "2-digit", minute: "2-digit", second: "2-digit" };
  if (spanMs < 36 * 3_600_000) return { hour: "2-digit", minute: "2-digit" };
  return { month: "short", day: "numeric", hour: "2-digit" };
}
