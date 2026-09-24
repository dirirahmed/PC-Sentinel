import type { Process } from "../types/api";

export type ProcessSortKey = "cpu" | "memory" | "name" | "pid";
export type SortDir = "asc" | "desc";

/** Case-insensitive match on name, PID or executable path. */
export function filterProcesses(list: Process[], query: string): Process[] {
  const q = query.trim().toLowerCase();
  if (!q) return list;
  return list.filter((p) => p.name.toLowerCase().includes(q) || String(p.pid) === q || p.path.toLowerCase().includes(q));
}

/**
 * Returns a sorted copy. Processes whose CPU couldn't be measured always sort
 * last so "unknown" never masquerades as "idle" at the top of an ascending list.
 */
export function sortProcesses(list: Process[], key: ProcessSortKey, dir: SortDir): Process[] {
  const sign = dir === "asc" ? 1 : -1;
  return [...list].sort((a, b) => {
    let cmp = 0;
    switch (key) {
      case "cpu":
        if (a.cpuPercent == null || b.cpuPercent == null) {
          if (a.cpuPercent == null && b.cpuPercent == null) break;
          return a.cpuPercent == null ? 1 : -1;
        }
        cmp = a.cpuPercent - b.cpuPercent;
        break;
      case "memory":
        cmp = a.memoryBytes - b.memoryBytes;
        break;
      case "name":
        cmp = a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
        break;
      case "pid":
        cmp = a.pid - b.pid;
        break;
    }
    return cmp !== 0 ? cmp * sign : a.pid - b.pid;
  });
}

/** Sensible first direction when a column header is clicked. */
export function defaultDir(key: ProcessSortKey): SortDir {
  return key === "name" || key === "pid" ? "asc" : "desc";
}
