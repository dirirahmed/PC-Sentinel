import { useCallback, useMemo, useState } from "react";
import { IconSearch } from "../components/icons";
import { Banner, Panel } from "../components/ui";
import { usePolling } from "../hooks/usePolling";
import { formatBytes, formatPercent } from "../lib/format";
import { defaultDir, filterProcesses, sortProcesses, type ProcessSortKey, type SortDir } from "../lib/processes";
import { api } from "../services/api";

const REFRESH_MS = 3000;
const MAX_ROWS = 300;

const COLUMNS: { key: ProcessSortKey | null; label: string; numeric?: boolean }[] = [
  { key: "name", label: "Name" },
  { key: "pid", label: "PID", numeric: true },
  { key: "cpu", label: "CPU", numeric: true },
  { key: "memory", label: "Memory", numeric: true },
  { key: null, label: "Status" },
  { key: null, label: "Path" },
];

export function Processes() {
  const fetcher = useCallback(() => api.processes(), []);
  const { data, error, loading } = usePolling(fetcher, REFRESH_MS);
  const [query, setQuery] = useState("");
  const [sortKey, setSortKey] = useState<ProcessSortKey>("cpu");
  const [dir, setDir] = useState<SortDir>("desc");

  const rows = useMemo(() => {
    const all = data?.processes ?? [];
    return sortProcesses(filterProcesses(all, query), sortKey, dir);
  }, [data, query, sortKey, dir]);

  const onSort = (key: ProcessSortKey) => {
    if (key === sortKey) setDir(dir === "asc" ? "desc" : "asc");
    else {
      setSortKey(key);
      setDir(defaultDir(key));
    }
  };

  return (
    <div className="page">
      <Banner tone="info">
        Read-only view. Ending or changing processes is intentionally not part of V1. CPU is shown as a share of the whole machine (100% =
        every core busy), like Task Manager.
      </Banner>
      {error && <Banner tone="warning">{error}</Banner>}
      <Panel
        title="Processes"
        aside={<span className="muted small num">{data ? `${rows.length} of ${data.count}` : loading ? "Loading…" : ""}</span>}
      >
        <div className="toolbar">
          <label className="search">
            <IconSearch />
            <input
              type="search"
              placeholder="Filter by name, PID or path"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="Filter processes"
            />
          </label>
          <label className="select">
            <span className="muted small">Sort by</span>
            <select
              value={sortKey}
              onChange={(e) => {
                const k = e.target.value as ProcessSortKey;
                setSortKey(k);
                setDir(defaultDir(k));
              }}
            >
              <option value="cpu">CPU</option>
              <option value="memory">Memory</option>
              <option value="name">Name</option>
              <option value="pid">PID</option>
            </select>
          </label>
        </div>
        <div className="table-wrap">
          <table className="table processes">
            <thead>
              <tr>
                {COLUMNS.map((c) => (
                  <th
                    key={c.label}
                    className={c.numeric ? "numeric" : ""}
                    aria-sort={c.key === sortKey ? (dir === "asc" ? "ascending" : "descending") : undefined}
                  >
                    {c.key ? (
                      <button className="th-sort" onClick={() => onSort(c.key!)}>
                        {c.label}
                        {c.key === sortKey && <span aria-hidden="true">{dir === "asc" ? " ▲" : " ▼"}</span>}
                      </button>
                    ) : (
                      c.label
                    )}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.slice(0, MAX_ROWS).map((p) => (
                <tr key={p.pid}>
                  <td className="proc-name" title={p.name}>
                    {p.name}
                  </td>
                  <td className="numeric num">{p.pid}</td>
                  <td className="numeric num">
                    {p.cpuPercent == null ? (
                      <span className="muted" title="Not measurable (access denied or just started)">
                        —
                      </span>
                    ) : (
                      formatPercent(p.cpuPercent, 1)
                    )}
                  </td>
                  <td className="numeric num">{formatBytes(p.memoryBytes)}</td>
                  <td className="muted">{p.status || "—"}</td>
                  <td className="proc-path mono" title={p.path}>
                    {p.path || (
                      <span className="muted" title="Protected, system or kernel process; the path needs elevated rights or does not exist">
                        Not accessible
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {rows.length > MAX_ROWS && <p className="muted small">Showing the first {MAX_ROWS} rows; refine the filter to see more.</p>}
          {data && rows.length === 0 && <p className="empty">No processes match “{query}”.</p>}
        </div>
      </Panel>
    </div>
  );
}
