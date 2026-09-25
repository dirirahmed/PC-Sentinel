import type {
  AlertsResponse,
  AnalysisResponse,
  AskResponse,
  ChatTurn,
  Config,
  MetricsResponse,
  ProcessesResponse,
  RangeKey,
  SettingsResponse,
  SystemResponse,
} from "../types/api";

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
// AI answers come from a remote model and can take much longer than telemetry.
const AI_TIMEOUT_MS = 100_000;

async function request<T>(path: string, init?: RequestInit, timeoutMs = TIMEOUT_MS): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
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
  analysis: () => request<AnalysisResponse>("/api/analysis"),
  ask: (question: string, history: ChatTurn[]) =>
    request<AskResponse>(
      "/api/ai/ask",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ question, history }),
      },
      AI_TIMEOUT_MS,
    ),
};
