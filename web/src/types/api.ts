// Mirrors the Go API response shapes (internal/api/dto.go, internal/models).
// `null` always means "not available on this machine", never zero.

export type Severity = "info" | "warning" | "critical";
export type Grade = "good" | "fair" | "poor" | "critical" | "unavailable";
export type SpaceStatus = "ok" | "warning" | "critical";

export interface HostInfo {
  hostname: string;
  os: string;
  platform: string;
  platformVersion: string;
  kernelArch: string;
  bootTime: string;
}

export interface CPUStats {
  model: string;
  physicalCores: number;
  logicalCores: number;
  baseFrequencyMHz: number | null;
  currentFrequencyMHz: number | null;
  usagePercent: number;
  perCorePercent: number[] | null;
  temperatureC: number | null;
  temperatureSource?: string;
}

export interface MemoryStats {
  totalBytes: number;
  usedBytes: number;
  availableBytes: number;
  usedPercent: number;
}

export interface GPUEngine {
  name: string;
  usagePercent: number;
}

export interface GPUStats {
  name: string;
  vendor: string;
  usagePercent: number | null;
  memoryUsedBytes: number | null;
  memoryTotalBytes: number | null;
  temperatureC: number | null;
  temperatureSource?: string;
  engines: GPUEngine[];
}

export interface GPUReport {
  available: boolean;
  reason?: string;
  source?: string;
  adapters: GPUStats[];
}

export interface DiskStats {
  mountpoint: string;
  device: string;
  fsType: string;
  totalBytes: number;
  usedBytes: number;
  freeBytes: number;
  usedPercent: number;
  freePercent: number;
  readBytesPerSec: number | null;
  writeBytesPerSec: number | null;
  spaceStatus: SpaceStatus;
}

export interface DiskIO {
  readBytesPerSec: number | null;
  writeBytesPerSec: number | null;
}

export interface NetworkStats {
  rxBytesPerSec: number;
  txBytesPerSec: number;
  totalRxBytes: number;
  totalTxBytes: number;
  interfaces: number;
}

export interface CollectorStatus {
  name: string;
  ok: boolean;
  error?: string;
}

export interface AgentStats {
  pid: number;
  cpuPercent: number;
  avgCpuPercent: number;
  memoryRssBytes: number;
  goHeapBytes: number;
  goroutines: number;
  uptimeSeconds: number;
  avgCollectionMillis: number;
  lastCollectionMillis: number;
  storageAvailable: boolean;
  storageError?: string;
}

export interface CategoryScore {
  key: string;
  label: string;
  available: boolean;
  score: number;
  grade: Grade;
  weight: number;
  reasons: string[];
}

export interface HealthReport {
  overall: number;
  grade: Grade;
  categories: CategoryScore[];
  summary: string;
}

export interface Alert {
  id: string;
  rule: string;
  component: string;
  target?: string;
  severity: Severity;
  title: string;
  reason: string;
  value: number;
  threshold: number;
  startedAt: string;
  updatedAt: string;
  resolvedAt: string | null;
}

export interface SystemResponse {
  host: HostInfo;
  timestamp: string;
  uptimeSeconds: number;
  health: HealthReport;
  cpu: CPUStats;
  memory: MemoryStats;
  gpu: GPUReport;
  disks: DiskStats[];
  diskIO: DiskIO;
  network: NetworkStats;
  activeAlerts: Alert[];
  collectors: CollectorStatus[];
  agent: AgentStats;
}

export interface HistoryPoint {
  t: string;
  cpu: number;
  cpuMax: number;
  memory: number;
  memoryUsed: number;
  gpu: number | null;
  gpuMax: number | null;
  cpuTemp: number | null;
  gpuTemp: number | null;
  netRx: number;
  netTx: number;
  diskRead: number | null;
  diskWrite: number | null;
}

export interface Stat {
  available: boolean;
  avg: number;
  peak: number;
  current: number;
}

export interface SeriesSummary {
  cpu: Stat;
  memory: Stat;
  gpu: Stat;
  cpuTemp: Stat;
  gpuTemp: Stat;
  netRx: Stat;
  netTx: Stat;
  diskRead: Stat;
  diskWrite: Stat;
}

export interface DiskUsagePoint {
  t: string;
  mountpoint: string;
  usedPercent: number;
  freeBytes: number;
}

export interface MetricsResponse {
  range: string;
  bucketSeconds: number;
  points: HistoryPoint[];
  summary: SeriesSummary;
  disks?: DiskUsagePoint[];
  disksError?: string;
}

export interface Process {
  pid: number;
  name: string;
  cpuPercent: number | null;
  memoryBytes: number;
  memoryPercent: number;
  path: string;
  status: string;
}

export interface ProcessesResponse {
  timestamp: string;
  count: number;
  processes: Process[];
  note: string;
}

export interface AlertsResponse {
  active: Alert[];
  history: Alert[];
  historyError?: string;
}

export interface Threshold {
  warning: number;
  critical: number;
  sustainSeconds: number;
}

export interface Config {
  listenAddress: string;
  port: number;
  sampleIntervalSeconds: number;
  historyIntervalSeconds: number;
  retentionDays: number;
  alertCooldownSeconds: number;
  thresholds: {
    cpuUsage: Threshold;
    memoryUsage: Threshold;
    diskFreePercent: Threshold;
    cpuTemperature: Threshold;
    gpuTemperature: Threshold;
    sustainedCpu: { averagePercent: number; windowMinutes: number };
  };
}

export interface SettingsResponse {
  config: Config;
  restartRequired?: string[];
}

export const RANGES = ["5m", "15m", "30m", "1h", "6h", "24h", "7d"] as const;
export type RangeKey = (typeof RANGES)[number];
