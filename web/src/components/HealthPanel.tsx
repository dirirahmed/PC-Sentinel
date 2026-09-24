import type { HealthReport } from "../types/api";
import { gradeLabel, gradeTone } from "../lib/status";
import { Meter, Panel, StatusPill } from "./ui";

export function HealthPanel({ health }: { health: HealthReport }) {
  const tone = gradeTone(health.grade);
  return (
    <Panel
      title="System health"
      aside={
        <a href="#/settings" className="subtle-link">
          How is this scored?
        </a>
      }
      className="health"
    >
      <div className="health-top">
        <div className={`health-score tone-text-${tone}`}>
          <span className="num">{health.overall}</span>
          <span className="of">/100</span>
        </div>
        <div>
          <StatusPill tone={tone}>{gradeLabel[health.grade]}</StatusPill>
          <p className="health-summary">{health.summary}</p>
        </div>
      </div>
      <ul className="health-cats">
        {health.categories.map((c) => (
          <li key={c.key} title={c.reasons.join("\n")}>
            <span className="cat-label">{c.label}</span>
            {c.available ? (
              <>
                <Meter value={c.score} tone={gradeTone(c.grade)} label={`${c.label} health`} />
                <span className="cat-score num">{c.score}</span>
              </>
            ) : (
              <>
                <span className="cat-na">{c.reasons[0]}</span>
                <span className="cat-score muted">—</span>
              </>
            )}
          </li>
        ))}
      </ul>
    </Panel>
  );
}
