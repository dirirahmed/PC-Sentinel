import type { AnalysisStatus, ChatTurn, Finding, SeriesSummary } from "../types/api";
import { formatPercent, formatRate } from "./format";
import { severityTone, type Tone } from "./status";

export const SUGGESTED_QUESTIONS = [
  "Why is my PC slow?",
  "What's causing the biggest problem?",
  "Is my PC CPU or GPU limited?",
  "What should I fix first?",
  "What can I do to improve performance?",
];

export const MAX_QUESTION_CHARS = 1000;
/** Earlier turns sent with each question (the server enforces the same cap). */
export const MAX_HISTORY_TURNS = 8;

export function analysisTone(status: AnalysisStatus): Tone {
  if (status === "ok") return "good";
  if (status === "collecting") return "muted";
  return severityTone(status);
}

export const analysisLabel: Record<AnalysisStatus, string> = {
  ok: "No issues",
  collecting: "Collecting data",
  info: "For your information",
  warning: "Needs attention",
  critical: "Action recommended",
};

/** Counts findings per severity for the summary line. */
export function countBySeverity(findings: Finding[]): Record<Finding["severity"], number> {
  const out = { critical: 0, warning: 0, info: 0 };
  for (const f of findings) out[f.severity]++;
  return out;
}

export interface MetricRow {
  label: string;
  avg: string;
  peak: string;
}

/** The measured averages the analysis was based on; unavailable data stays "Unavailable". */
export function metricRows(m: SeriesSummary): MetricRow[] {
  const pct = (s: SeriesSummary["cpu"]) => ({
    avg: s.available ? formatPercent(s.avg) : "Unavailable",
    peak: s.available ? formatPercent(s.peak) : "—",
  });
  const diskAvailable = m.diskRead.available || m.diskWrite.available;
  const diskAvg = (m.diskRead.available ? m.diskRead.avg : 0) + (m.diskWrite.available ? m.diskWrite.avg : 0);
  return [
    { label: "CPU", ...pct(m.cpu) },
    { label: "Memory", ...pct(m.memory) },
    { label: "GPU", ...pct(m.gpu) },
    { label: "Disk I/O", avg: diskAvailable ? formatRate(diskAvg) : "Unavailable", peak: "—" },
  ];
}

export function validateQuestion(q: string): string | null {
  const t = q.trim();
  if (!t) return "Type a question first.";
  if (t.length > MAX_QUESTION_CHARS) return `Questions are limited to ${MAX_QUESTION_CHARS} characters.`;
  return null;
}

export interface ChatMessage extends ChatTurn {
  id: number;
  error?: boolean;
}

/**
 * Earlier conversation to send with a new question: successful turns only,
 * the most recent MAX_HISTORY_TURNS, starting with a user turn and ending
 * with an assistant turn (a failed question leaves an unanswered user turn,
 * which is dropped).
 */
export function buildHistory(messages: ChatMessage[], maxTurns = MAX_HISTORY_TURNS): ChatTurn[] {
  const turns: ChatTurn[] = [];
  for (let i = 0; i < messages.length; i++) {
    const m = messages[i];
    if (m.error) continue;
    if (m.role === "user") {
      const reply = messages[i + 1];
      if (!reply || reply.role !== "assistant" || reply.error) continue;
    }
    turns.push({ role: m.role, content: m.content });
  }
  let out = turns.slice(-maxTurns);
  while (out.length > 0 && out[0].role !== "user") out = out.slice(1);
  return out;
}
