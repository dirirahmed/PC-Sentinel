import type { ReactNode } from "react";
import type { Severity } from "../types/api";
import { severityTone, type Tone } from "../lib/status";
import { IconCheck, IconCritical, IconInfo, IconWarning } from "./icons";

export function Panel({
  title,
  aside,
  children,
  className = "",
}: {
  title?: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      {(title || aside) && (
        <header className="panel-head">
          {title && <h2>{title}</h2>}
          {aside && <div className="panel-aside">{aside}</div>}
        </header>
      )}
      {children}
    </section>
  );
}

const toneIcon: Record<Tone, (p: { className?: string }) => ReactNode> = {
  good: IconCheck,
  warning: IconWarning,
  serious: IconWarning,
  critical: IconCritical,
  info: IconInfo,
  muted: IconInfo,
};

/** Status is always icon + label, never color alone. */
export function StatusPill({ tone, children }: { tone: Tone; children: ReactNode }) {
  const Icon = toneIcon[tone];
  return (
    <span className={`pill tone-${tone}`}>
      <Icon className="pill-icon" />
      {children}
    </span>
  );
}

export function SeverityBadge({ severity }: { severity: Severity }) {
  return <StatusPill tone={severityTone(severity)}>{severity[0].toUpperCase() + severity.slice(1)}</StatusPill>;
}

export function Meter({ value, tone = "neutral", label }: { value: number; tone?: Tone | "neutral"; label: string }) {
  const pct = Math.max(0, Math.min(100, value));
  return (
    <div
      className={`meter meter-${tone}`}
      role="meter"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      aria-label={label}
    >
      <div className="meter-fill" style={{ width: `${pct}%` }} />
    </div>
  );
}

export function Unavailable({ reason }: { reason?: string }) {
  return (
    <span className="unavailable" title={reason}>
      Unavailable
    </span>
  );
}

export function KeyValue({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="kv">
      {items.map(([k, v]) => (
        <div key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Banner({ tone, children }: { tone: Tone; children: ReactNode }) {
  const Icon = toneIcon[tone];
  return (
    <div className={`banner tone-${tone}`} role={tone === "critical" ? "alert" : "status"}>
      <Icon className="banner-icon" />
      <div>{children}</div>
    </div>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <p className="empty">{children}</p>;
}
