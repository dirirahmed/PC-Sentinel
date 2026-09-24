import { describe, expect, it } from "vitest";
import { axisTimeFormat, linePath, niceBytesMax, linearScale, nearestIndex, niceMax, ticks } from "./chart";

describe("niceMax", () => {
  it("rounds up to a readable axis maximum", () => {
    expect(niceMax(0)).toBe(1);
    expect(niceMax(7)).toBe(10);
    expect(niceMax(1.7)).toBe(2);
    expect(niceMax(230)).toBe(250);
    expect(niceMax(4.1e6)).toBe(5e6);
  });
});

describe("niceBytesMax", () => {
  it("rounds in binary units", () => {
    expect(niceBytesMax(14.2 * 1024)).toBe(20 * 1024);
    expect(niceBytesMax(1.9 * 1024 ** 2)).toBe(2 * 1024 ** 2);
    expect(niceBytesMax(300)).toBe(500);
  });
});

describe("linePath", () => {
  const id = (v: number) => v;
  it("draws a continuous line", () => {
    expect(
      linePath(
        [
          { x: 0, y: 1 },
          { x: 1, y: 2 },
        ],
        id,
        id,
      ),
    ).toBe("M0.0,1.0L1.0,2.0");
  });
  it("breaks the line at missing values", () => {
    const d = linePath(
      [
        { x: 0, y: 1 },
        { x: 1, y: null },
        { x: 2, y: 3 },
      ],
      id,
      id,
    );
    expect(d).toBe("M0.0,1.0M2.0,3.0");
  });
  it("breaks the line across collection gaps", () => {
    const d = linePath(
      [
        { x: 0, y: 1 },
        { x: 10, y: 1 },
        { x: 100, y: 1 },
      ],
      id,
      id,
      20,
    );
    expect(d.match(/M/g)).toHaveLength(2);
  });
});

describe("scales and ticks", () => {
  it("maps domain to range, including inverted ranges", () => {
    const s = linearScale([0, 100], [200, 0]);
    expect(s(0)).toBe(200);
    expect(s(50)).toBe(100);
  });
  it("produces evenly spaced ticks", () => {
    expect(ticks(100)).toEqual([0, 25, 50, 75, 100]);
    expect(ticks(5e6)).toEqual([0, 1e6, 2e6, 3e6, 4e6, 5e6]);
    expect(ticks(250)).toEqual([0, 50, 100, 150, 200, 250]);
  });
  it("finds the nearest sample for hover", () => {
    expect(nearestIndex([0, 10, 20, 30], 14)).toBe(1);
    expect(nearestIndex([0, 10, 20, 30], 16)).toBe(2);
    expect(nearestIndex([0, 10], 99)).toBe(1);
    expect(nearestIndex([], 5)).toBe(-1);
  });
});

describe("axisTimeFormat", () => {
  it("adds seconds for short spans and dates for long ones", () => {
    expect("second" in axisTimeFormat(5 * 60_000)).toBe(true);
    expect("second" in axisTimeFormat(6 * 3_600_000)).toBe(false);
    expect("day" in axisTimeFormat(7 * 86_400_000)).toBe(true);
  });
});
