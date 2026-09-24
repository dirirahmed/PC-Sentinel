import type { ReactNode } from "react";
import { Sparkline } from "./charts";

export function MetricTile({
  label,
  value,
  unit,
  lines,
  spark,
  sparkMax,
  color,
  href,
}: {
  label: string;
  value: ReactNode;
  unit?: string;
  lines: ReactNode[];
  spark?: (number | null)[];
  sparkMax?: number;
  color: string;
  href?: string;
}) {
  const body = (
    <>
      <div className="tile-label">
        <span className="swatch" style={{ background: color }} />
        {label}
      </div>
      <div className="tile-value num">
        {value}
        {unit && <span className="tile-unit">{unit}</span>}
      </div>
      <ul className="tile-lines">
        {lines.map((l, i) => (
          <li key={i}>{l}</li>
        ))}
      </ul>
      {spark && <Sparkline values={spark} max={sparkMax} color={color} label={`${label}, last 5 minutes`} />}
    </>
  );
  return href ? (
    <a className="tile" href={href}>
      {body}
    </a>
  ) : (
    <div className="tile">{body}</div>
  );
}
