import { useCallback, useEffect, useState } from "react";

export type ThemePreference = "system" | "light" | "dark";

const STORAGE_KEY = "pgpt-theme";

// localStorage can throw (private mode, sandboxed iframes, storage-partitioned
// contexts, enterprise policy). Mirror index.html's try/catch guard so the
// always-mounted theme hook never crashes the app on read/write.
function readStoredTheme(): ThemePreference {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return stored === "light" || stored === "dark" ? stored : "system";
  } catch {
    return "system";
  }
}

function writeStoredTheme(next: ThemePreference) {
  try {
    if (next === "system") {
      localStorage.removeItem(STORAGE_KEY);
    } else {
      localStorage.setItem(STORAGE_KEY, next);
    }
  } catch {
    /* persistence unavailable — the in-memory preference still applies */
  }
}

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
  const [preference, setPreference] = useState<ThemePreference>(readStoredTheme);

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
    writeStoredTheme(next);
    setPreference(next);
  }, []);

  return { preference, setTheme };
}
