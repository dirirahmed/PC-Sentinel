import { useCallback, useEffect, useRef, useState } from "react";

export interface PollState<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  lastUpdated: number | null;
  refresh: () => void;
}

/**
 * Calls `fetcher` immediately and then every `intervalMs` after the previous
 * call settles (never overlapping). Polling pauses while the tab is hidden,
 * so a minimised dashboard costs the agent nothing. On error the last good
 * data is kept and the error is exposed alongside it.
 */
export function usePolling<T>(fetcher: () => Promise<T>, intervalMs: number, key = ""): PollState<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [lastUpdated, setLastUpdated] = useState<number | null>(null);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const run = async () => {
      if (document.hidden) {
        timer = setTimeout(run, intervalMs);
        return;
      }
      try {
        const result = await fetcherRef.current();
        if (cancelled) return;
        setData(result);
        setError(null);
        setLastUpdated(Date.now());
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!cancelled) {
          setLoading(false);
          timer = setTimeout(run, intervalMs);
        }
      }
    };

    const onVisible = () => {
      if (!document.hidden) {
        clearTimeout(timer);
        run();
      }
    };

    setLoading(true);
    run();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [intervalMs, key, tick]);

  return { data, error, loading, lastUpdated, refresh };
}
