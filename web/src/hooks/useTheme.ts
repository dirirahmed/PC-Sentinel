import { useEffect, useState } from "react";

export type ThemePref = "system" | "dark" | "light";
const KEY = "pcsentinel.theme";

function read(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "dark" || v === "light" || v === "system") return v;
  } catch {
    // Storage can be unavailable (privacy mode); fall back to the OS setting.
  }
  return "system";
}

/** Theme is a per-browser display preference, so it lives in localStorage, not the agent config. */
export function useTheme(): [ThemePref, (t: ThemePref) => void] {
  const [pref, setPref] = useState<ThemePref>(read);
  useEffect(() => {
    const root = document.documentElement;
    if (pref === "system") root.removeAttribute("data-theme");
    else root.setAttribute("data-theme", pref);
    try {
      localStorage.setItem(KEY, pref);
    } catch {
      /* ignore */
    }
  }, [pref]);
  return [pref, setPref];
}
