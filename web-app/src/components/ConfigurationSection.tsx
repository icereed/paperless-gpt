import {
  CheckCircleIcon,
  ChevronDownIcon,
  LockClosedIcon,
  MagnifyingGlassIcon,
} from "@heroicons/react/24/outline";
import axios from "axios";
import classNames from "classnames";
import React, { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

interface ConfigEntry {
  name: string;
  category: string;
  description: string;
  secret: boolean;
  default: string;
  source: "env" | "saved" | "default";
  is_set: boolean;
  value?: string;
  editable_at?: string;
}

interface ConfigResponse {
  categories: string[];
  entries: ConfigEntry[];
}

const sourceChip: Record<ConfigEntry["source"], { label: string; className: string }> = {
  env: { label: "env", className: "bg-primary-tint text-ink" },
  saved: { label: "saved — overrides env", className: "bg-warn-tint text-warn" },
  default: { label: "default", className: "bg-surface-2 text-faint" },
};

// Inline `code` spans in registry descriptions, rendered without a full md parser.
const renderDescription = (text: string): React.ReactNode =>
  text.split(/(`[^`]+`)/g).map((part, i) =>
    part.startsWith("`") && part.endsWith("`") ? (
      <code
        key={i}
        className="rounded bg-surface-2 px-1 py-0.5 font-mono text-[0.8em]"
      >
        {part.slice(1, -1)}
      </code>
    ) : (
      <React.Fragment key={i}>{part}</React.Fragment>
    )
  );

const ConfigRow: React.FC<{ entry: ConfigEntry }> = ({ entry }) => {
  const chip = sourceChip[entry.source];
  return (
    <div className="border-t border-line py-3 first:border-t-0">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <code className="font-mono text-sm font-medium">{entry.name}</code>
        <span
          className={classNames(
            "rounded-full px-2 py-0.5 text-xs font-medium",
            chip.className
          )}
        >
          {chip.label}
        </span>
        {entry.secret && (
          <span className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 text-xs font-medium text-muted">
            <LockClosedIcon className="h-3 w-3" aria-hidden="true" />
            secret
          </span>
        )}
        {entry.editable_at && (
          <Link
            to={entry.editable_at}
            className="text-xs font-medium text-primary hover:underline"
          >
            editable in app →
          </Link>
        )}
      </div>

      <div className="mt-1 flex flex-wrap items-baseline gap-x-2 text-sm">
        {entry.secret ? (
          <span
            className={classNames(
              "inline-flex items-center gap-1",
              entry.is_set ? "text-pos" : "text-faint"
            )}
          >
            {entry.is_set && <CheckCircleIcon className="h-4 w-4" aria-hidden="true" />}
            {entry.is_set ? "set" : "not set"}
          </span>
        ) : entry.value ? (
          <span className="break-all font-mono text-ink">{entry.value}</span>
        ) : (
          <span className="text-faint">
            {entry.default ? (
              <>
                using default:{" "}
                <span className="font-mono">{entry.default}</span>
              </>
            ) : (
              "not set"
            )}
          </span>
        )}
      </div>

      <p className="mt-1 max-w-prose text-xs leading-relaxed text-muted">
        {renderDescription(entry.description)}
      </p>
    </div>
  );
};

/**
 * Read-only diagnostics view of every active setting — env vars, their
 * effective values, source (env / saved / default) and meaning. Secrets show
 * only whether they are set. This is the "what is this instance actually
 * running with?" surface for hands-off operators.
 */
const ConfigurationSection: React.FC = () => {
  const [config, setConfig] = useState<ConfigResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState(true);

  useEffect(() => {
    let cancelled = false;
    axios
      .get<ConfigResponse>("./api/config")
      .then((res) => {
        if (!cancelled) setConfig(res.data);
      })
      .catch((err) => {
        console.error("Failed to load configuration:", err);
        if (!cancelled) setError("Could not load the configuration view.");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const grouped = useMemo(() => {
    if (!config) return [];
    const q = query.trim().toLowerCase();
    const match = (e: ConfigEntry) =>
      !q ||
      e.name.toLowerCase().includes(q) ||
      e.description.toLowerCase().includes(q);
    return config.categories
      .map((category) => ({
        category,
        entries: config.entries.filter((e) => e.category === category && match(e)),
      }))
      .filter((g) => g.entries.length > 0);
  }, [config, query]);

  const overriddenCount = useMemo(
    () => config?.entries.filter((e) => e.source === "saved").length ?? 0,
    [config]
  );

  return (
    <section
      aria-labelledby="config-heading"
      className="rounded-lg border border-line bg-surface p-6"
    >
      <button
        type="button"
        onClick={() => setCollapsed((v) => !v)}
        aria-expanded={!collapsed}
        className="flex w-full items-center justify-between gap-2 text-left"
      >
        <span>
          <span id="config-heading" className="text-lg font-semibold">
            Active configuration
          </span>
          <span className="ml-2 text-sm text-muted">
            what this instance is running with
          </span>
        </span>
        <ChevronDownIcon
          className={classNames(
            "h-5 w-5 shrink-0 text-muted transition-transform duration-150 ease-out-quart",
            !collapsed && "rotate-180"
          )}
          aria-hidden="true"
        />
      </button>

      {!collapsed && (
        <div className="mt-4">
          <p className="max-w-prose text-sm text-muted">
            Every environment variable paperless-gpt understands, its effective
            value and where it comes from. Secrets show only whether they are
            set — values are never exposed. This view is read-only; change
            values via your environment (restart required) or the linked in-app
            editors.
          </p>

          {overriddenCount > 0 && (
            <p className="mt-3 rounded-md bg-warn-tint px-3 py-2 text-xs text-warn">
              {overriddenCount} value{overriddenCount === 1 ? "" : "s"} saved in
              the app currently override your environment variables (marked{" "}
              <span className="font-medium">saved</span> below).
            </p>
          )}

          <div className="relative mt-4">
            <MagnifyingGlassIcon
              className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-faint"
              aria-hidden="true"
            />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter by name or description…"
              aria-label="Filter configuration"
              className="h-9 w-full rounded-md border border-line bg-surface pl-9 pr-3 text-sm"
            />
          </div>

          {error && (
            <p role="alert" className="mt-4 text-sm text-neg">
              {error}
            </p>
          )}

          {!config && !error && (
            <div className="mt-4 space-y-2" aria-busy="true">
              {[0, 1, 2].map((i) => (
                <div key={i} className="h-14 animate-pulse rounded bg-surface-2" />
              ))}
              <span className="sr-only">Loading configuration…</span>
            </div>
          )}

          {config && grouped.length === 0 && (
            <p className="mt-4 text-sm text-muted">
              No settings match “{query}”.
            </p>
          )}

          <div className="mt-4 space-y-6">
            {grouped.map(({ category, entries }) => (
              <div key={category}>
                <h3 className="text-xs font-semibold uppercase tracking-wide text-faint">
                  {category}
                </h3>
                <div className="mt-1">
                  {entries.map((entry) => (
                    <ConfigRow key={entry.name} entry={entry} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
};

export default ConfigurationSection;
