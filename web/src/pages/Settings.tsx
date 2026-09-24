import { useEffect, useState, type ReactNode } from "react";
import { Banner, Panel } from "../components/ui";
import type { ThemePref } from "../hooks/useTheme";
import { api } from "../services/api";
import type { Config, Threshold } from "../types/api";

type ThresholdKey = "cpuUsage" | "memoryUsage" | "cpuTemperature" | "gpuTemperature";

const THRESHOLD_ROWS: { key: ThresholdKey; label: string; unit: string; max: number }[] = [
  { key: "cpuUsage", label: "CPU usage", unit: "%", max: 100 },
  { key: "memoryUsage", label: "Memory usage", unit: "%", max: 100 },
  { key: "cpuTemperature", label: "CPU temperature", unit: "°C", max: 125 },
  { key: "gpuTemperature", label: "GPU temperature", unit: "°C", max: 125 },
];

function NumberField({
  label,
  value,
  onChange,
  min,
  max,
  unit,
  hint,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
  min: number;
  max: number;
  unit?: string;
  hint?: ReactNode;
}) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      <span className="field-input">
        <input
          type="number"
          value={Number.isFinite(value) ? value : ""}
          min={min}
          max={max}
          step="any"
          onChange={(e) => onChange(e.target.valueAsNumber)}
        />
        {unit && <span className="unit">{unit}</span>}
      </span>
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function Settings({
  theme,
  setTheme,
  onSaved,
}: {
  theme: ThemePref;
  setTheme: (t: ThemePref) => void;
  onSaved: (c: Config) => void;
}) {
  const [cfg, setCfg] = useState<Config | null>(null);
  const [saved, setSaved] = useState<Config | null>(null);
  const [status, setStatus] = useState<{ tone: "good" | "critical" | "warning"; msg: string } | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api
      .settings()
      .then((r) => {
        setCfg(r.config);
        setSaved(r.config);
      })
      .catch((e: Error) => setStatus({ tone: "critical", msg: e.message }));
  }, []);

  if (!cfg)
    return (
      <div className="page">{status ? <Banner tone={status.tone}>{status.msg}</Banner> : <p className="muted">Loading settings…</p>}</div>
    );

  const set = (patch: Partial<Config>) => setCfg({ ...cfg, ...patch });
  const setTh = (key: ThresholdKey | "diskFreePercent", patch: Partial<Threshold>) =>
    setCfg({ ...cfg, thresholds: { ...cfg.thresholds, [key]: { ...cfg.thresholds[key], ...patch } } });
  const dirty = JSON.stringify(cfg) !== JSON.stringify(saved);
  // Empty number inputs become NaN, which JSON encodes as null.
  const incomplete = JSON.stringify(cfg).includes("null");

  const save = async () => {
    setSaving(true);
    setStatus(null);
    try {
      const r = await api.saveSettings(cfg);
      setCfg(r.config);
      setSaved(r.config);
      onSaved(r.config);
      setStatus(
        r.restartRequired?.length
          ? { tone: "warning", msg: `Saved. Restart PC Sentinel to apply: ${r.restartRequired.join(", ")}.` }
          : { tone: "good", msg: "Saved. Changes are active now." },
      );
    } catch (e) {
      setStatus({ tone: "critical", msg: e instanceof Error ? e.message : String(e) });
    } finally {
      setSaving(false);
    }
  };

  const t = cfg.thresholds;
  return (
    <div className="page settings">
      <div className="grid-2">
        <Panel title="Monitoring">
          <div className="fields">
            <NumberField
              label="Refresh interval"
              unit="s"
              min={1}
              max={60}
              value={cfg.sampleIntervalSeconds}
              onChange={(v) => set({ sampleIntervalSeconds: v })}
              hint="How often telemetry is collected and the dashboard updates."
            />
            <NumberField
              label="History resolution"
              unit="s"
              min={cfg.sampleIntervalSeconds}
              max={600}
              value={cfg.historyIntervalSeconds}
              onChange={(v) => set({ historyIntervalSeconds: v })}
              hint="Samples are averaged into one stored row per interval."
            />
            <NumberField
              label="History retention"
              unit="days"
              min={1}
              max={90}
              value={cfg.retentionDays}
              onChange={(v) => set({ retentionDays: v })}
              hint="Older history and resolved alerts are deleted hourly."
            />
            <NumberField
              label="Alert cooldown"
              unit="s"
              min={0}
              max={3600}
              value={cfg.alertCooldownSeconds}
              onChange={(v) => set({ alertCooldownSeconds: v })}
              hint="Minimum quiet time before the same alert can fire again."
            />
          </div>
        </Panel>
        <Panel title="Appearance & server">
          <div className="fields">
            <label className="field">
              <span className="field-label">Theme</span>
              <select value={theme} onChange={(e) => setTheme(e.target.value as ThemePref)}>
                <option value="system">Match system</option>
                <option value="dark">Dark</option>
                <option value="light">Light</option>
              </select>
              <span className="field-hint">Stored in this browser only.</span>
            </label>
            <label className="field">
              <span className="field-label">Listen address</span>
              <input type="text" value={cfg.listenAddress} onChange={(e) => set({ listenAddress: e.target.value })} />
              <span className="field-hint">Keep 127.0.0.1 so the API is only reachable from this PC. Requires restart.</span>
            </label>
            <NumberField
              label="API port"
              min={1}
              max={65535}
              value={cfg.port}
              onChange={(v) => set({ port: v })}
              hint="Requires restart."
            />
          </div>
        </Panel>
      </div>

      <Panel
        title="Alert thresholds"
        aside={<span className="muted small">An alert fires only after the value stays past the threshold for the sustain time.</span>}
      >
        <div className="table-wrap">
          <table className="table thresholds">
            <thead>
              <tr>
                <th>Metric</th>
                <th>Warning</th>
                <th>Critical</th>
                <th>Sustain (s)</th>
              </tr>
            </thead>
            <tbody>
              {THRESHOLD_ROWS.map((r) => (
                <tr key={r.key}>
                  <td>{r.label} ≥</td>
                  <td>
                    <NumInput value={t[r.key].warning} unit={r.unit} max={r.max} onChange={(v) => setTh(r.key, { warning: v })} />
                  </td>
                  <td>
                    <NumInput value={t[r.key].critical} unit={r.unit} max={r.max} onChange={(v) => setTh(r.key, { critical: v })} />
                  </td>
                  <td>
                    <NumInput value={t[r.key].sustainSeconds} max={3600} onChange={(v) => setTh(r.key, { sustainSeconds: v })} />
                  </td>
                </tr>
              ))}
              <tr>
                <td>Drive free space ≤</td>
                <td>
                  <NumInput
                    value={t.diskFreePercent.warning}
                    unit="%"
                    max={99}
                    onChange={(v) => setTh("diskFreePercent", { warning: v })}
                  />
                </td>
                <td>
                  <NumInput
                    value={t.diskFreePercent.critical}
                    unit="%"
                    max={99}
                    onChange={(v) => setTh("diskFreePercent", { critical: v })}
                  />
                </td>
                <td className="muted small">immediate</td>
              </tr>
              <tr>
                <td>Sustained CPU average ≥ (info)</td>
                <td>
                  <NumInput
                    value={t.sustainedCpu.averagePercent}
                    unit="%"
                    max={100}
                    onChange={(v) => set({ thresholds: { ...t, sustainedCpu: { ...t.sustainedCpu, averagePercent: v } } })}
                  />
                </td>
                <td className="muted small">over window</td>
                <td>
                  <NumInput
                    value={t.sustainedCpu.windowMinutes}
                    unit="min"
                    max={25}
                    onChange={(v) => set({ thresholds: { ...t, sustainedCpu: { ...t.sustainedCpu, windowMinutes: v } } })}
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </Panel>

      <div className="save-bar">
        {status && <Banner tone={status.tone}>{status.msg}</Banner>}
        <div className="save-actions">
          <button
            className="btn"
            disabled={!dirty || saving}
            onClick={() => {
              setCfg(saved);
              setStatus(null);
            }}
          >
            Discard
          </button>
          <button
            className="btn primary"
            disabled={!dirty || saving || incomplete}
            onClick={save}
            title={incomplete ? "Fill in every field" : undefined}
          >
            {saving ? "Saving…" : "Save settings"}
          </button>
        </div>
      </div>

      <div className="grid-2">
        <Panel title="How the health score works">
          <ul className="method">
            <li>
              <strong>CPU</strong>: 5-minute average. Full score up to 50%, falling linearly to 40 at 95%.
            </li>
            <li>
              <strong>Memory</strong>: current use. Full score up to 70%, falling to 35 at 95%.
            </li>
            <li>
              <strong>Disk</strong>: the fullest drive. Full score with ≥20% free, falling to 25 at 5% free.
            </li>
            <li>
              <strong>GPU</strong>: 5-minute average. Full score up to 70%, falling gently to 60 at 98%, because heavy GPU use is normal in
              games.
            </li>
            <li>
              <strong>Temperature</strong>: hottest CPU/GPU reading. Full score up to 70°C, falling to 25 at 95°C.
            </li>
            <li>
              <strong>Overall</strong>: weighted average of available categories (CPU 25, memory 25, disk 20, temperature 20, GPU 10),
              capped at 25 points above the worst category.
            </li>
          </ul>
          <p className="muted small">
            These are fixed rules of thumb for a typical desktop, not a precise measure of hardware health. Categories without sensor data
            are left out rather than guessed.
          </p>
        </Panel>
        <Panel title="Privacy">
          <p className="small">
            All telemetry is collected and stored on this PC. PC Sentinel makes no network requests of its own, has no cloud component and
            uses no AI or machine-learning models. It reads interface byte counters only and never inspects network traffic. Process names
            and paths are shown live but are not written to the history database.
          </p>
        </Panel>
      </div>
    </div>
  );
}

function NumInput({ value, onChange, unit, max }: { value: number; onChange: (v: number) => void; unit?: string; max: number }) {
  return (
    <span className="field-input compact">
      <input
        type="number"
        value={Number.isFinite(value) ? value : ""}
        min={0}
        max={max}
        step="any"
        onChange={(e) => onChange(e.target.valueAsNumber)}
      />
      {unit && <span className="unit">{unit}</span>}
    </span>
  );
}
