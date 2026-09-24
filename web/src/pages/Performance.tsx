import { useCallback, useMemo, useState } from "react";
import { TimeSeriesChart, type Series } from "../components/charts";
import { RangeSelector } from "../components/RangeSelector";
import { Banner, Panel } from "../components/ui";
import { usePolling } from "../hooks/usePolling";
import { formatBytes, formatPercent, formatRate, formatTemp } from "../lib/format";
import { api } from "../services/api";
import type { RangeKey, Stat } from "../types/api";

const LIVE_RANGES: RangeKey[] = ["5m", "15m", "30m"];

function StatLine({ label, stat, fmt }: { label?: string; stat: Stat; fmt: (v: number) => string }) {
  if (!stat.available) return <span className="muted small">No data in range</span>;
  return (
    <span className="stat-line num">
      {label && <span className="muted">{label} </span>}
      avg <strong>{fmt(stat.avg)}</strong> · peak <strong>{fmt(stat.peak)}</strong>
    </span>
  );
}

const pct = (v: number | null) => formatPercent(v, 1);
const pctAxis = (v: number) => `${v}%`;
const rateAxis = (v: number) => (v === 0 ? "0" : formatBytes(v));
const tempAxis = (v: number) => `${Math.round(v)}°`;

export function Performance({ intervalMs }: { intervalMs: number }) {
  const [range, setRange] = useState<RangeKey>("15m");
  const live = LIVE_RANGES.includes(range);
  const fetcher = useCallback(() => api.metrics(range, true), [range]);
  // Live ranges refresh at the sample rate; stored history changes slowly.
  const { data, error, loading } = usePolling(fetcher, live ? intervalMs : 30_000, range);

  const view = useMemo(() => {
    const pts = data?.range === range ? data.points : [];
    const times = pts.map((p) => Date.parse(p.t));
    const gapMs = (data?.bucketSeconds ?? 2) * 3000;
    const mountpoints = [...new Set((data?.disks ?? []).map((d) => d.mountpoint))];
    const diskTimes = [...new Set((data?.disks ?? []).map((d) => Date.parse(d.t)))].sort((a, b) => a - b);
    const diskSeries: Series[] = mountpoints.slice(0, 3).map((m, i) => {
      const byT = new Map((data?.disks ?? []).filter((d) => d.mountpoint === m).map((d) => [Date.parse(d.t), d.usedPercent]));
      return { key: m, label: m, color: `var(--cat-${i + 1})`, values: diskTimes.map((t) => byT.get(t) ?? null) };
    });
    return { pts, times, gapMs, diskTimes, diskSeries, extraDrives: Math.max(0, mountpoints.length - 3) };
  }, [data, range]);

  const { pts, times, gapMs } = view;
  const s = data?.summary;
  const common = { times, maxGapMs: gapMs };

  return (
    <div className="page">
      <div className="toolbar">
        <RangeSelector value={range} onChange={setRange} />
        <span className="muted small">
          {loading && !data ? "Loading…" : live ? "Live, full resolution" : `Stored history, ${data?.bucketSeconds ?? "–"}s buckets`}
        </span>
      </div>
      {error && (
        <Banner tone="warning">
          {error}. {live ? "" : "Long ranges need the history database; see the System panel on the dashboard."}
        </Banner>
      )}

      <div className="grid-2">
        <Panel title="CPU utilization" aside={s && <StatLine stat={s.cpu} fmt={(v) => formatPercent(v)} />}>
          <TimeSeriesChart
            {...common}
            yMax={100}
            format={pct}
            axisFormat={pctAxis}
            series={[{ key: "cpu", label: "CPU", color: "var(--series-cpu)", values: pts.map((p) => p.cpu) }]}
          />
        </Panel>
        <Panel title="Memory usage" aside={s && <StatLine stat={s.memory} fmt={(v) => formatPercent(v)} />}>
          <TimeSeriesChart
            {...common}
            yMax={100}
            format={pct}
            axisFormat={pctAxis}
            series={[{ key: "mem", label: "Memory", color: "var(--series-mem)", values: pts.map((p) => p.memory) }]}
          />
        </Panel>
        <Panel title="GPU utilization" aside={s && <StatLine stat={s.gpu} fmt={(v) => formatPercent(v)} />}>
          <TimeSeriesChart
            {...common}
            yMax={100}
            format={pct}
            axisFormat={pctAxis}
            emptyText="GPU utilization is not available on this system"
            series={[{ key: "gpu", label: "GPU", color: "var(--series-gpu)", values: pts.map((p) => p.gpu) }]}
          />
        </Panel>
        <Panel title="Temperature" aside={s && <StatLine label="CPU" stat={s.cpuTemp} fmt={formatTemp} />}>
          <TimeSeriesChart
            {...common}
            format={formatTemp}
            axisFormat={tempAxis}
            emptyText="No readable temperature sensors on this system"
            series={[
              { key: "cpuTemp", label: "CPU", color: "var(--series-cputemp)", values: pts.map((p) => p.cpuTemp) },
              { key: "gpuTemp", label: "GPU", color: "var(--series-gputemp)", values: pts.map((p) => p.gpuTemp) },
            ]}
          />
        </Panel>
        <Panel
          title="Network"
          aside={
            s && (
              <span className="stat-pair">
                <StatLine label="↓" stat={s.netRx} fmt={(v) => formatRate(v)} />
                <StatLine label="↑" stat={s.netTx} fmt={(v) => formatRate(v)} />
              </span>
            )
          }
        >
          <TimeSeriesChart
            {...common}
            bytes
            format={formatRate}
            axisFormat={rateAxis}
            series={[
              { key: "rx", label: "Download", color: "var(--series-rx)", values: pts.map((p) => p.netRx) },
              { key: "tx", label: "Upload", color: "var(--series-tx)", values: pts.map((p) => p.netTx) },
            ]}
          />
        </Panel>
        <Panel
          title="Disk I/O"
          aside={
            s && (
              <span className="stat-pair">
                <StatLine label="R" stat={s.diskRead} fmt={(v) => formatRate(v)} />
                <StatLine label="W" stat={s.diskWrite} fmt={(v) => formatRate(v)} />
              </span>
            )
          }
        >
          <TimeSeriesChart
            {...common}
            bytes
            format={formatRate}
            axisFormat={rateAxis}
            emptyText="Disk I/O counters are not available on this system"
            series={[
              { key: "read", label: "Read", color: "var(--series-read)", values: pts.map((p) => p.diskRead) },
              { key: "write", label: "Write", color: "var(--series-write)", values: pts.map((p) => p.diskWrite) },
            ]}
          />
        </Panel>
      </div>

      <Panel
        title="Drive fill level"
        aside={
          <span className="muted small">
            Sampled every minute{view.extraDrives > 0 ? ` · ${view.extraDrives} more drive(s) not shown` : ""}
          </span>
        }
      >
        {data?.disksError ? (
          <p className="empty">Drive history unavailable: {data.disksError}</p>
        ) : (
          <TimeSeriesChart
            times={view.diskTimes}
            showLegend
            emptyText="Drive history needs the history database"
            yMax={100}
            format={pct}
            axisFormat={pctAxis}
            series={view.diskSeries}
            height={180}
          />
        )}
      </Panel>
    </div>
  );
}
