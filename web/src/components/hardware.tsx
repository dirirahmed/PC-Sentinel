import type { DiskStats, GPUReport } from "../types/api";
import { formatBytes, formatPercent, formatRate, formatTemp } from "../lib/format";
import { spaceTone } from "../lib/status";
import { EmptyState, Meter, StatusPill, Unavailable } from "./ui";

export function DriveList({ disks }: { disks: DiskStats[] }) {
  if (disks.length === 0) return <EmptyState>No drives reported.</EmptyState>;
  return (
    <ul className="drive-list">
      {disks.map((d) => {
        const tone = spaceTone(d.spaceStatus);
        return (
          <li key={d.mountpoint}>
            <div className="drive-head">
              <span className="drive-name mono">{d.mountpoint}</span>
              <span className="muted small">{d.fsType}</span>
              {d.spaceStatus !== "ok" && (
                <StatusPill tone={tone}>{d.spaceStatus === "critical" ? "Critically low space" : "Low space"}</StatusPill>
              )}
              <span className="drive-free num">
                {formatBytes(d.freeBytes)} free of {formatBytes(d.totalBytes)}
              </span>
            </div>
            <Meter value={d.usedPercent} tone={d.spaceStatus === "ok" ? "neutral" : tone} label={`${d.mountpoint} used`} />
            <div className="drive-foot small muted num">
              <span>{formatPercent(d.usedPercent)} used</span>
              <span>
                R {d.readBytesPerSec == null ? "—" : formatRate(d.readBytesPerSec)} · W{" "}
                {d.writeBytesPerSec == null ? "—" : formatRate(d.writeBytesPerSec)}
              </span>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

/** Per-core utilization as a compact heat strip; cell opacity follows load. */
export function CoreGrid({ cores }: { cores: number[] | null }) {
  if (!cores || cores.length === 0) return <p className="muted small">Per-core data unavailable.</p>;
  return (
    <div className="core-grid" role="list" aria-label="Per-core utilization">
      {cores.map((v, i) => (
        <div key={i} className="core" role="listitem" title={`Core ${i}: ${v.toFixed(0)}%`}>
          <div className="core-fill" style={{ height: `${Math.max(2, v)}%` }} />
          <span className="core-label num">{i}</span>
        </div>
      ))}
    </div>
  );
}

export function GpuDetails({ gpu }: { gpu: GPUReport }) {
  if (!gpu.available || gpu.adapters.length === 0) {
    return (
      <div className="gpu-na">
        <Unavailable reason={gpu.reason} />
        <p className="muted small">{gpu.reason}</p>
      </div>
    );
  }
  return (
    <div className="gpu-list">
      {gpu.adapters.map((g) => (
        <div key={g.name} className="gpu">
          <div className="gpu-head">
            <span className="gpu-name">{g.name}</span>
            <span className="tag">{g.vendor}</span>
          </div>
          <div className="gpu-stats">
            <div>
              <span className="label">Utilization</span>
              <span className="num">{g.usagePercent == null ? <Unavailable /> : formatPercent(g.usagePercent)}</span>
            </div>
            <div>
              <span className="label">Dedicated memory</span>
              <span className="num">
                {g.memoryUsedBytes == null ? <Unavailable /> : formatBytes(g.memoryUsedBytes)}
                {g.memoryTotalBytes != null && <span className="muted"> / {formatBytes(g.memoryTotalBytes)}</span>}
              </span>
            </div>
            <div>
              <span className="label">Temperature</span>
              <span className="num" title={g.temperatureSource}>
                {g.temperatureC == null ? (
                  <Unavailable reason="No vendor-neutral GPU temperature API on Windows" />
                ) : (
                  formatTemp(g.temperatureC)
                )}
              </span>
            </div>
          </div>
          {g.engines.length > 0 && (
            <ul className="engines">
              {g.engines.slice(0, 5).map((e) => (
                <li key={e.name}>
                  <span className="small">{e.name}</span>
                  <Meter value={e.usagePercent} label={`${e.name} engine`} />
                  <span className="num small">{formatPercent(e.usagePercent)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}
      {gpu.reason && <p className="muted small">{gpu.reason}</p>}
    </div>
  );
}
