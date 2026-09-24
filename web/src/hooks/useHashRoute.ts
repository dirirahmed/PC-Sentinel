import { useEffect, useState } from "react";

export const ROUTES = ["dashboard", "performance", "processes", "alerts", "settings"] as const;
export type Route = (typeof ROUTES)[number];

export function parseRoute(hash: string): Route {
  const name = hash.replace(/^#\/?/, "").split(/[/?]/)[0];
  return (ROUTES as readonly string[]).includes(name) ? (name as Route) : "dashboard";
}

/** Five pages don't justify a router dependency; the URL hash is enough. */
export function useHashRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.hash));
  useEffect(() => {
    const onChange = () => setRoute(parseRoute(window.location.hash));
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);
  return route;
}
