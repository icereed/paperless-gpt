import {
  ComputerDesktopIcon,
  MoonIcon,
  SunIcon,
} from "@heroicons/react/24/outline";
import React from "react";
import { ThemePreference, useTheme } from "../hooks/useTheme";

const order: ThemePreference[] = ["system", "light", "dark"];

const meta: Record<
  ThemePreference,
  { label: string; icon: React.ComponentType<React.SVGProps<SVGSVGElement>> }
> = {
  system: { label: "System theme", icon: ComputerDesktopIcon },
  light: { label: "Light theme", icon: SunIcon },
  dark: { label: "Dark theme", icon: MoonIcon },
};

interface ThemeToggleProps {
  showLabel?: boolean;
}

/** Cycles system → light → dark. */
const ThemeToggle: React.FC<ThemeToggleProps> = ({ showLabel = true }) => {
  const { preference, setTheme } = useTheme();
  const next = order[(order.indexOf(preference) + 1) % order.length];
  const Icon = meta[preference].icon;
  const label = `${meta[preference].label} — switch to ${meta[next].label.toLowerCase()}`;

  return (
    <button
      type="button"
      onClick={() => setTheme(next)}
      aria-label={label}
      title={label}
      className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm text-muted transition-colors duration-150 ease-out-quart hover:bg-surface hover:text-ink"
    >
      <Icon className="h-5 w-5 shrink-0" aria-hidden="true" />
      {showLabel && <span>{meta[preference].label}</span>}
    </button>
  );
};

export default ThemeToggle;
