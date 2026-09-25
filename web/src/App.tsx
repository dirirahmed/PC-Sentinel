import { useCallback, useEffect, useState, type ComponentType } from "react";
import { IconAlerts, IconAnalysis, IconDashboard, IconPerformance, IconProcesses, IconSettings, Logo } from "./components/icons";
import { Banner } from "./components/ui";
import { useHashRoute, type Route } from "./hooks/useHashRoute";
import { usePolling } from "./hooks/usePolling";
import { useTheme } from "./hooks/useTheme";
import { formatBytes, formatDuration } from "./lib/format";
import { gradeLabel, gradeTone } from "./lib/status";
import { Alerts } from "./pages/Alerts";
import { Analysis } from "./pages/Analysis";
import { Dashboard } from "./pages/Dashboard";
import { Performance } from "./pages/Performance";
import { Processes } from "./pages/Processes";
import { Settings } from "./pages/Settings";
import { api } from "./services/api";
import type { Config } from "./types/api";

const NAV: { route: Route; label: string; Icon: ComponentType }[] = [
  { route: "dashboard", label: "Dashboard", Icon: IconDashboard },
  { route: "performance", label: "Performance", Icon: IconPerformance },
  { route: "analysis", label: "Analysis", Icon: IconAnalysis },
  { route: "processes", label: "Processes", Icon: IconProcesses },
  { route: "alerts", label: "Alerts", Icon: IconAlerts },
  { route: "settings", label: "Settings", Icon: IconSettings },
];

const DEFAULT_INTERVAL_MS = 2000;

export function App() {
  const route = useHashRoute();
  const [theme, setTheme] = useTheme();
  const [intervalMs, setIntervalMs] = useState(DEFAULT_INTERVAL_MS);

  useEffect(() => {
    api
      .settings()
      .then((r) => setIntervalMs(r.config.sampleIntervalSeconds * 1000))
      .catch(() => {
        /* keep the default; the connection banner reports failures */
      });
  }, []);

  const fetchSystem = useCallback(() => api.system(), []);
  const system = usePolling(fetchSystem, intervalMs);
  const sys = system.data;
  const activeCount = sys?.activeAlerts.length ?? 0;
  const title = NAV.find((n) => n.route === route)?.label ?? "Dashboard";

  useEffect(() => {
    document.title = activeCount > 0 ? `(${activeCount}) PC Sentinel` : "PC Sentinel";
  }, [activeCount]);

  const onSaved = (c: Config) => setIntervalMs(c.sampleIntervalSeconds * 1000);

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <Logo />
          <span>PC Sentinel</span>
        </div>
        <nav>
          {NAV.map(({ route: r, label, Icon }) => (
            <a key={r} href={`#/${r}`} className={r === route ? "active" : ""} aria-current={r === route ? "page" : undefined}>
              <Icon />
              <span>{label}</span>
              {r === "alerts" && activeCount > 0 && <span className="badge num">{activeCount}</span>}
            </a>
          ))}
        </nav>
        {sys && (
          <div className="sidebar-foot small">
            <div className="muted">Agent footprint</div>
            <div className="num">
              {sys.agent.avgCpuPercent.toFixed(2)}% CPU avg · {formatBytes(sys.agent.memoryRssBytes)}
            </div>
          </div>
        )}
      </aside>

      <main className="main">
        <header className="topbar">
          <div>
            <h1>{title}</h1>
            {sys && (
              <p className="muted small">
                {sys.host.hostname} · {sys.host.platform} {sys.host.platformVersion} · up {formatDuration(sys.uptimeSeconds)}
              </p>
            )}
          </div>
          <div className="topbar-right">
            {sys && (
              <a href="#/dashboard" className={`health-chip tone-${gradeTone(sys.health.grade)}`} title={sys.health.summary}>
                Health <strong className="num">{sys.health.overall}</strong> {gradeLabel[sys.health.grade]}
              </a>
            )}
            <span className={`live ${system.error ? "offline" : ""}`}>
              <span className="dot" />
              {system.error ? "Disconnected" : `Live · ${intervalMs / 1000}s`}
            </span>
          </div>
        </header>

        {system.error && (
          <Banner tone="critical">
            {system.error}. {sys ? "Showing the last values received; retrying automatically." : "Make sure pcsentinel is running."}
          </Banner>
        )}

        {route === "dashboard" &&
          (sys ? <Dashboard system={sys} /> : !system.error && <p className="muted page">Collecting the first sample…</p>)}
        {route === "performance" && <Performance intervalMs={intervalMs} />}
        {route === "analysis" && <Analysis />}
        {route === "processes" && <Processes />}
        {route === "alerts" && <Alerts />}
        {route === "settings" && <Settings theme={theme} setTheme={setTheme} onSaved={onSaved} />}
      </main>
    </div>
  );
}
