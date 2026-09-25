import { describe, expect, it } from "vitest";
import type { Finding, SeriesSummary, Stat } from "../types/api";
import { analysisTone, buildHistory, countBySeverity, metricRows, validateQuestion, type ChatMessage } from "./analysis";

const stat = (avg: number, peak = avg, available = true): Stat => ({ available, avg, peak, current: avg });
const none: Stat = { available: false, avg: 0, peak: 0, current: 0 };

describe("analysis status", () => {
  it("maps statuses onto the shared tones", () => {
    expect(analysisTone("ok")).toBe("good");
    expect(analysisTone("collecting")).toBe("muted");
    expect(analysisTone("info")).toBe("info");
    expect(analysisTone("warning")).toBe("warning");
    expect(analysisTone("critical")).toBe("critical");
  });

  it("counts findings by severity", () => {
    const f = (severity: Finding["severity"]) => ({ severity }) as Finding;
    expect(countBySeverity([f("critical"), f("warning"), f("warning")])).toEqual({ critical: 1, warning: 2, info: 0 });
  });
});

describe("metricRows", () => {
  it("shows measured averages and never invents missing data", () => {
    const m = {
      cpu: stat(91.4, 100),
      memory: stat(40),
      gpu: none,
      diskRead: stat(1024 * 1024),
      diskWrite: none,
    } as unknown as SeriesSummary;
    const rows = metricRows(m);
    expect(rows.map((r) => r.label)).toEqual(["CPU", "Memory", "GPU", "Disk I/O"]);
    expect(rows[0].avg).toBe("91%");
    expect(rows[0].peak).toBe("100%");
    expect(rows[2].avg).toBe("Unavailable");
    expect(rows[3].avg).toBe("1.0 MB/s");
  });
});

describe("validateQuestion", () => {
  it("rejects empty and over-long questions", () => {
    expect(validateQuestion("   ")).toBe("Type a question first.");
    expect(validateQuestion("a".repeat(1001))).toBe("Questions are limited to 1000 characters.");
    expect(validateQuestion(" Why is my PC slow? ")).toBe(null);
  });
});

describe("buildHistory", () => {
  const msg = (id: number, role: ChatMessage["role"], content: string, error = false): ChatMessage => ({ id, role, content, error });

  it("keeps answered turns and drops failed questions", () => {
    const history = buildHistory([
      msg(1, "user", "Why is my PC slow?"),
      msg(2, "assistant", "CPU is busy."),
      msg(3, "user", "What should I fix first?"),
      msg(4, "assistant", "Network error", true),
    ]);
    expect(history).toEqual([
      { role: "user", content: "Why is my PC slow?" },
      { role: "assistant", content: "CPU is busy." },
    ]);
  });

  it("caps the history and always starts with a user turn", () => {
    const msgs: ChatMessage[] = [];
    for (let i = 0; i < 6; i++) {
      msgs.push(msg(i * 2, "user", `q${i}`), msg(i * 2 + 1, "assistant", `a${i}`));
    }
    const history = buildHistory(msgs, 3);
    expect(history).toHaveLength(2);
    expect(history[0]).toEqual({ role: "user", content: "q5" });
    expect(buildHistory([])).toEqual([]);
  });
});
