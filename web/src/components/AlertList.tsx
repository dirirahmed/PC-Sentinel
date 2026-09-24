import type { Alert } from "../types/api";
import { formatDuration, formatRelative } from "../lib/format";
import { EmptyState, SeverityBadge } from "./ui";

export function AlertList({ alerts, compact = false }: { alerts: Alert[]; compact?: boolean }) {
  if (alerts.length === 0) {
    return <EmptyState>No active alerts. Thresholds are configurable in Settings.</EmptyState>;
  }
  return (
    <ul className={`alert-list ${compact ? "compact" : ""}`}>
      {alerts.map((a) => (
        <li key={a.id} className={`alert-item sev-${a.severity}`}>
          <div className="alert-head">
            <SeverityBadge severity={a.severity} />
            <span className="alert-title">{a.title}</span>
            <span className="alert-time" title={new Date(a.startedAt).toLocaleString()}>
              {a.resolvedAt
                ? `lasted ${formatDuration((Date.parse(a.resolvedAt) - Date.parse(a.startedAt)) / 1000)}`
                : formatRelative(a.startedAt)}
            </span>
          </div>
          {!compact && <p className="alert-reason">{a.reason}</p>}
          <div className="alert-meta">
            <span className="tag">{a.component}</span>
            {a.target && <span className="tag">{a.target}</span>}
          </div>
        </li>
      ))}
    </ul>
  );
}
