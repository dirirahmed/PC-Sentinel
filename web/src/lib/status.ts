import type { Grade, Severity, SpaceStatus } from "../types/api";

/** Visual tones map onto the reserved status palette in styles.css. */
export type Tone = "good" | "warning" | "serious" | "critical" | "info" | "muted";

export function gradeTone(grade: Grade): Tone {
  switch (grade) {
    case "good":
      return "good";
    case "fair":
      return "warning";
    case "poor":
      return "serious";
    case "critical":
      return "critical";
    default:
      return "muted";
  }
}

export function severityTone(s: Severity): Tone {
  return s === "critical" ? "critical" : s === "warning" ? "warning" : "info";
}

export function spaceTone(s: SpaceStatus): Tone {
  return s === "critical" ? "critical" : s === "warning" ? "warning" : "good";
}

export const gradeLabel: Record<Grade, string> = {
  good: "Good",
  fair: "Fair",
  poor: "Poor",
  critical: "Critical",
  unavailable: "Unavailable",
};
