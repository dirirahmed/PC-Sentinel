import type { AlertsResponse, Config, MetricsResponse, ProcessesResponse, RangeKey, SettingsResponse, SystemResponse } from "../types/api";

class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

const TIMEOUT_MS = 10_000;

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(path, { ...init, signal: controller.signal });
  } catch (err) {
    const aborted = err instanceof DOMException && err.name === "AbortError";
    throw new ApiError(aborted ? "Request timed out" : "Cannot reach the PC Sentinel agent", 0);
  } finally {
    clearTimeout(timer);
  }
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const msg = body && typeof body.error === "string" ? body.error : `HTTP ${res.status}`;
    throw new ApiError(msg, res.status);
  }
  return body as T;
}

export const api = {
  system: () => request<SystemResponse>("/api/system"),
  metrics: (range: RangeKey, withDisks = false) => request<MetricsResponse>(`/api/metrics?range=${range}${withDisks ? "&disks=1" : ""}`),
  processes: () => request<ProcessesResponse>("/api/processes"),
  alerts: (limit = 200) => request<AlertsResponse>(`/api/alerts?limit=${limit}`),
  settings: () => request<SettingsResponse>("/api/settings"),
  saveSettings: (config: Config) =>
    request<SettingsResponse>("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(config),
    }),
};
