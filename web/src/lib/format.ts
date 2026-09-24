const UNAVAILABLE = "Unavailable";

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

/** Formats bytes with binary (1024) steps, as Windows Explorer does. */
export function formatBytes(bytes: number | null | undefined, digits = 1): string {
  if (bytes == null || !Number.isFinite(bytes)) return UNAVAILABLE;
  if (bytes < 1024) return `${Math.max(0, Math.round(bytes))} B`;
  let v = bytes;
  let unit = 0;
  while (v >= 1024 && unit < BYTE_UNITS.length - 1) {
    v /= 1024;
    unit++;
  }
  return `${v.toFixed(v >= 100 ? 0 : digits)} ${BYTE_UNITS[unit]}`;
}

export function formatRate(bytesPerSec: number | null | undefined): string {
  if (bytesPerSec == null) return UNAVAILABLE;
  return `${formatBytes(bytesPerSec)}/s`;
}

export function formatPercent(v: number | null | undefined, digits = 0): string {
  if (v == null || !Number.isFinite(v)) return UNAVAILABLE;
  return `${v.toFixed(digits)}%`;
}

export function formatTemp(c: number | null | undefined): string {
  if (c == null) return UNAVAILABLE;
  return `${Math.round(c)}°C`;
}

export function formatMHz(mhz: number | null | undefined): string {
  if (mhz == null) return UNAVAILABLE;
  return mhz >= 1000 ? `${(mhz / 1000).toFixed(2)} GHz` : `${Math.round(mhz)} MHz`;
}

/** 93784 -> "1d 2h 3m" (drops seconds once the value is over an hour). */
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}d ${h}h ${m}m`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${sec}s`;
  return `${sec}s`;
}

export function formatRelative(iso: string, now: number = Date.now()): string {
  const diff = Math.round((now - new Date(iso).getTime()) / 1000);
  if (diff < 5) return "just now";
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}
