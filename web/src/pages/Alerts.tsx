import { useCallback, useMemo, useState } from "react";
import { AlertList } from "../components/AlertList";
import { Banner, EmptyState, Panel, SeverityBadge } from "../components/ui";
import { usePolling } from "../hooks/usePolling";
import { formatDuration } from "../lib/format";
import { api } from "../services/api";
import type { Severity } from "../types/api";

const SEVERITIES: (Severity | "all")[] = ["all", "critical", "warning", "info"];

export function Alerts() {
  const fetcher = useCallback(() => api.alerts(500), []);
  const { data, error } = usePolling(fetcher, 5000);
  const [filter, setFilter] = useState<Severity | "all">("all");
  const history = useMemo(() => (data?.history ?? []).filter((a) => filter === "all" || a.severity === filter), [data, filter]);

  return (
    <div className="page">
      {error && <Banner tone="warning">{error}</Banner>}
      <Panel title="Current alerts" aside={<span className="muted small num">{data?.active.length ?? 0} active</span>}>
        <AlertList alerts={data?.active ?? []} />
      </Panel>
      <Panel
        title="Alert history"
        aside={
          <div className="segmented" role="radiogroup" aria-label="Severity filter">
            {SEVERITIES.map((s) => (
              <button
                key={s}
                role="radio"
                aria-checked={s === filter}
                className={s === filter ? "active" : ""}
                onClick={() => setFilter(s)}
              >
                {s}
              </button>
            ))}
          </div>
        }
      >
        {data?.historyError && <Banner tone="warning">Stored history unavailable: {data.historyError}</Banner>}
        {history.length === 0 ? (
          <EmptyState>No alerts recorded in the retention period.</EmptyState>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>Severity</th>
                  <th>Alert</th>
                  <th>Component</th>
                  <th>Reason</th>
                  <th>Started</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                {history.map((a) => {
                  const end = a.resolvedAt ? Date.parse(a.resolvedAt) : Date.now();
                  return (
                    <tr key={a.id}>
                      <td>
                        <SeverityBadge severity={a.severity} />
                      </td>
                      <td>{a.title}</td>
                      <td>
                        <span className="tag">{a.component}</span>
                      </td>
                      <td className="reason">{a.reason}</td>
                      <td className="num nowrap">{new Date(a.startedAt).toLocaleString()}</td>
                      <td className="num nowrap">
                        {formatDuration((end - Date.parse(a.startedAt)) / 1000)}
                        {!a.resolvedAt && <span className="tone-text-warning"> · ongoing</span>}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </div>
  );
}
