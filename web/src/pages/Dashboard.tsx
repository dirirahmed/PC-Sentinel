import { useCallback } from "react";
import { AlertList } from "../components/AlertList";
import { CoreGrid, DriveList, GpuDetails } from "../components/hardware";
import { HealthPanel } from "../components/HealthPanel";
import { MetricTile } from "../components/MetricTile";
import { KeyValue, Panel, StatusPill, Unavailable } from "../components/ui";
import { usePolling } from "../hooks/usePolling";
import { formatBytes, formatDuration, formatMHz, formatPercent, formatRate, formatTemp } from "../lib/format";
import { api } from "../services/api";
import type { GPUStats, SystemResponse } from "../types/api";

const SPARK_REFRESH_MS = 5000;

function busiestGPU(adapters: GPUStats[]): GPUStats | undefined {
  return [...adapters].sort((a, b) => (b.usagePercent ?? -1) - (a.usagePercent ?? -1))[0];
}

export function Dashboard({ system }: { system: SystemResponse }) {
  const fetchMetrics = useCallback(() => api.metrics("5m"), []);
  const { data: metrics } = usePolling(fetchMetrics, SPARK_REFRESH_MS);
  const pts = metrics?.points ?? [];
  const sum = metrics?.summary;
  const { cpu, memory, gpu, network, diskIO, disks, agent } = system;
  const primaryGPU = busiestGPU(gpu.adapters);
  const lowestFree = [...disks].sort((a, b) => a.freePercent - b.freePercent)[0];
  const peakAvg = (s?: { available: boolean; avg: number; peak: number }) =>
    s?.available ? `5 min peak ${formatPercent(s.peak)} · avg ${formatPercent(s.avg)}` : "5 min history building…";

  return (
    <div className="page dashboard">
      <div className="grid-2">
        <HealthPanel health={system.health} />
        <Panel
          title="Active alerts"
          aside={
            <a href="#/alerts" className="subtle-link">
              History →
            </a>
          }
        >
          <AlertList alerts={system.activeAlerts} compact={system.activeAlerts.length > 3} />
        </Panel>
      </div>

      <div className="tiles">
        <MetricTile
          label="CPU"
          href="#/performance"
          color="var(--series-cpu)"
          value={cpu.usagePercent.toFixed(0)}
          unit="%"
          lines={[
            peakAvg(sum?.cpu),
            <>
              {formatMHz(cpu.currentFrequencyMHz ?? cpu.baseFrequencyMHz)} ·{" "}
              {cpu.temperatureC == null ? <Unavailable reason="No readable CPU temperature sensor" /> : formatTemp(cpu.temperatureC)}
            </>,
          ]}
          spark={pts.map((p) => p.cpu)}
          sparkMax={100}
        />
        <MetricTile
          label="Memory"
          href="#/performance"
          color="var(--series-mem)"
          value={memory.usedPercent.toFixed(0)}
          unit="%"
          lines={[
            `${formatBytes(memory.usedBytes)} of ${formatBytes(memory.totalBytes)}`,
            `${formatBytes(memory.availableBytes)} available · peak ${sum?.memory.available ? formatPercent(sum.memory.peak) : "—"}`,
          ]}
          spark={pts.map((p) => p.memory)}
          sparkMax={100}
        />
        <MetricTile
          label="GPU"
          href="#/performance"
          color="var(--series-gpu)"
          value={primaryGPU?.usagePercent != null ? primaryGPU.usagePercent.toFixed(0) : <Unavailable reason={gpu.reason} />}
          unit={primaryGPU?.usagePercent != null ? "%" : undefined}
          lines={
            primaryGPU
              ? [
                  <span className="truncate" title={primaryGPU.name}>
                    {primaryGPU.name}
                  </span>,
                  `VRAM ${primaryGPU.memoryUsedBytes == null ? "n/a" : formatBytes(primaryGPU.memoryUsedBytes)} · ${primaryGPU.temperatureC == null ? "temp n/a" : formatTemp(primaryGPU.temperatureC)}`,
                ]
              : [<span className="muted">{gpu.reason ?? "No GPU detected"}</span>]
          }
          spark={pts.some((p) => p.gpu != null) ? pts.map((p) => p.gpu) : undefined}
          sparkMax={100}
        />
        <MetricTile
          label="Network ↓"
          href="#/performance"
          color="var(--series-rx)"
          value={formatRate(network.rxBytesPerSec)}
          lines={[
            `↑ ${formatRate(network.txBytesPerSec)} upload`,
            `Total ↓ ${formatBytes(network.totalRxBytes)} · ↑ ${formatBytes(network.totalTxBytes)}`,
          ]}
          spark={pts.map((p) => p.netRx)}
        />
        <MetricTile
          label="Disk"
          href="#/performance"
          color="var(--series-read)"
          value={
            diskIO.readBytesPerSec == null ? (
              <Unavailable reason="Disk I/O counters unavailable" />
            ) : (
              formatRate((diskIO.readBytesPerSec ?? 0) + (diskIO.writeBytesPerSec ?? 0))
            )
          }
          lines={[
            `R ${formatRate(diskIO.readBytesPerSec)} · W ${formatRate(diskIO.writeBytesPerSec)}`,
            lowestFree ? `Lowest free: ${lowestFree.mountpoint} ${formatPercent(lowestFree.freePercent)}` : "No drives",
          ]}
          spark={pts.some((p) => p.diskRead != null) ? pts.map((p) => (p.diskRead ?? 0) + (p.diskWrite ?? 0)) : undefined}
        />
      </div>

      <div className="grid-2">
        <Panel
          title="Processor"
          aside={
            <span className="muted small">
              {cpu.physicalCores} cores · {cpu.logicalCores} threads
            </span>
          }
        >
          <p className="cpu-model">{cpu.model}</p>
          <CoreGrid cores={cpu.perCorePercent} />
          <KeyValue
            items={[
              ["Current clock", formatMHz(cpu.currentFrequencyMHz)],
              ["Base clock", formatMHz(cpu.baseFrequencyMHz)],
              [
                "Temperature",
                cpu.temperatureC == null ? (
                  <Unavailable reason="No readable CPU temperature sensor" />
                ) : (
                  <span title={cpu.temperatureSource}>{formatTemp(cpu.temperatureC)}</span>
                ),
              ],
            ]}
          />
          {cpu.temperatureSource && <p className="muted small">Temperature source: {cpu.temperatureSource}</p>}
        </Panel>
        <Panel title="Drives">
          <DriveList disks={disks} />
        </Panel>
      </div>

      <div className="grid-2">
        <Panel title="Graphics" aside={gpu.source && <span className="muted small">{gpu.source}</span>}>
          <GpuDetails gpu={gpu} />
        </Panel>
        <Panel title="System">
          <KeyValue
            items={[
              ["Host", system.host.hostname],
              ["OS", `${system.host.platform} ${system.host.platformVersion} (${system.host.kernelArch})`],
              ["Uptime", formatDuration(system.uptimeSeconds)],
              ["Booted", new Date(system.host.bootTime).toLocaleString()],
              ["Agent footprint", `${agent.avgCpuPercent.toFixed(2)}% CPU (avg since start) · ${formatBytes(agent.memoryRssBytes)} RAM`],
              ["Collection time", `${agent.lastCollectionMillis.toFixed(1)} ms (avg ${agent.avgCollectionMillis.toFixed(1)} ms)`],
              [
                "History storage",
                agent.storageAvailable ? "OK" : <span className="tone-text-warning">{agent.storageError ?? "Unavailable"}</span>,
              ],
            ]}
          />
          <div className="collectors">
            {system.collectors.map((c) => (
              <span key={c.name} title={c.error}>
                <StatusPill tone={c.ok ? "good" : "warning"}>{c.name}</StatusPill>
              </span>
            ))}
          </div>
        </Panel>
      </div>
    </div>
  );
}
