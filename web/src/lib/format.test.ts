import { describe, expect, it } from "vitest";
import { formatBytes, formatDuration, formatMHz, formatPercent, formatRate, formatRelative, formatTemp } from "./format";

describe("formatBytes", () => {
  it("uses binary units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1023)).toBe("1023 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1536 * 1024 * 1024)).toBe("1.5 GB");
    expect(formatBytes(512 * 1024 ** 3)).toBe("512 GB");
  });

  it("reports missing values as unavailable instead of zero", () => {
    expect(formatBytes(null)).toBe("Unavailable");
    expect(formatRate(undefined)).toBe("Unavailable");
    expect(formatPercent(null)).toBe("Unavailable");
    expect(formatTemp(null)).toBe("Unavailable");
    expect(formatMHz(null)).toBe("Unavailable");
  });
});

describe("formatRate and friends", () => {
  it("formats per-second rates", () => {
    expect(formatRate(2 * 1024 * 1024)).toBe("2.0 MB/s");
  });
  it("formats percentages, temperatures and clocks", () => {
    expect(formatPercent(42.456, 1)).toBe("42.5%");
    expect(formatTemp(71.6)).toBe("72°C");
    expect(formatMHz(3600)).toBe("3.60 GHz");
    expect(formatMHz(800)).toBe("800 MHz");
  });
});

describe("formatDuration", () => {
  it("picks the two most useful units", () => {
    expect(formatDuration(42)).toBe("42s");
    expect(formatDuration(125)).toBe("2m 5s");
    expect(formatDuration(3 * 3600 + 120)).toBe("3h 2m");
    expect(formatDuration(93784)).toBe("1d 2h 3m");
    expect(formatDuration(-5)).toBe("0s");
  });
});

describe("formatRelative", () => {
  const now = Date.parse("2026-01-01T12:00:00Z");
  it("describes recent times", () => {
    expect(formatRelative("2026-01-01T11:59:58Z", now)).toBe("just now");
    expect(formatRelative("2026-01-01T11:59:00Z", now)).toBe("1m ago");
    expect(formatRelative("2026-01-01T09:00:00Z", now)).toBe("3h ago");
    expect(formatRelative("2025-12-30T12:00:00Z", now)).toBe("2d ago");
  });
});
