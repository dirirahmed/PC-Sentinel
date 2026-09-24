import { describe, expect, it } from "vitest";
import type { Process } from "../types/api";
import { defaultDir, filterProcesses, sortProcesses } from "./processes";

const p = (pid: number, name: string, cpu: number | null, mem: number, path = ""): Process => ({
  pid,
  name,
  cpuPercent: cpu,
  memoryBytes: mem,
  memoryPercent: 0,
  path,
  status: "",
});

const list = [
  p(10, "chrome.exe", 12, 900, "C:\\Program Files\\Google\\Chrome\\chrome.exe"),
  p(4, "System", null, 10),
  p(22, "Code.exe", 30, 500),
  p(7, "audiodg.exe", 0.5, 50),
];

describe("filterProcesses", () => {
  it("matches name case-insensitively", () => {
    expect(filterProcesses(list, "CODE").map((x) => x.pid)).toEqual([22]);
  });
  it("matches exact PID and path fragments", () => {
    expect(filterProcesses(list, "7").map((x) => x.pid)).toEqual([7]);
    expect(filterProcesses(list, "program files").map((x) => x.pid)).toEqual([10]);
  });
  it("returns everything for a blank query", () => {
    expect(filterProcesses(list, "  ")).toHaveLength(4);
  });
});

describe("sortProcesses", () => {
  it("sorts by CPU descending with unmeasured processes last", () => {
    expect(sortProcesses(list, "cpu", "desc").map((x) => x.pid)).toEqual([22, 10, 7, 4]);
  });
  it("keeps unmeasured CPU last when ascending too", () => {
    expect(sortProcesses(list, "cpu", "asc").map((x) => x.pid)).toEqual([7, 10, 22, 4]);
  });
  it("sorts by memory and by name", () => {
    expect(sortProcesses(list, "memory", "desc")[0].name).toBe("chrome.exe");
    expect(sortProcesses(list, "name", "asc").map((x) => x.name)).toEqual(["audiodg.exe", "chrome.exe", "Code.exe", "System"]);
  });
  it("does not mutate its input", () => {
    const copy = [...list];
    sortProcesses(list, "pid", "asc");
    expect(list).toEqual(copy);
  });
  it("chooses a natural first direction per column", () => {
    expect(defaultDir("cpu")).toBe("desc");
    expect(defaultDir("name")).toBe("asc");
  });
});
