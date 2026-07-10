import { useCallback, useEffect, useState } from "react";

export type ThemePreference = "system" | "light" | "dark";

const STORAGE_KEY = "pgpt-theme";

function applyPreference(preference: ThemePreference) {
  const dark =
    preference === "dark" ||
    (preference === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
}

/**
 * Three-way theme preference (system / light / dark), persisted in localStorage.
 * index.html applies the stored value before first paint; this hook keeps it
 * live afterwards and follows OS changes while in "system" mode.
 */
export function useTheme() {
  const [preference, setPreference] = useState<ThemePreference>(() => {
    const stored = localStorage.getItem(STORAGE_KEY);
    return stored === "light" || stored === "dark" ? stored : "system";
  });

  useEffect(() => {
    applyPreference(preference);
    if (preference === "system") {
      const mq = window.matchMedia("(prefers-color-scheme: dark)");
      const onChange = () => applyPreference("system");
      mq.addEventListener("change", onChange);
      return () => mq.removeEventListener("change", onChange);
    }
  }, [preference]);

  const setTheme = useCallback((next: ThemePreference) => {
    if (next === "system") {
      localStorage.removeItem(STORAGE_KEY);
    } else {
      localStorage.setItem(STORAGE_KEY, next);
    }
    setPreference(next);
  }, []);

  return { preference, setTheme };
}
