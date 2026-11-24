import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import axios from "axios";
import React, { useEffect, useState } from "react";

interface PersistenceIssue {
  dir: string;
  path: string;
  holds: string;
  fix: string;
}

/**
 * Warns when what the user creates on this page would be lost on the next
 * container update, because the directory holding it is not a volume.
 * `dirs` limits the warning to the directories this page writes to.
 */
const PersistenceWarning: React.FC<{ dirs?: string[] }> = ({ dirs }) => {
  const [issues, setIssues] = useState<PersistenceIssue[]>([]);

  useEffect(() => {
    axios
      .get<{ issues: PersistenceIssue[] }>("./api/persistence")
      .then((res) => setIssues(res.data.issues ?? []))
      .catch(() => setIssues([]));
  }, []);

  const shown = dirs ? issues.filter((i) => dirs.includes(i.dir)) : issues;
  if (shown.length === 0) return null;
  // The legacy /root/prompts mount is not a missing volume but a wrong one.
  const missing = shown.filter((i) => i.holds);
  const legacy = shown.filter((i) => !i.holds);

  return (
    <div className="flex gap-3 rounded-md border border-warn bg-warn-tint px-4 py-3 text-sm" role="alert">
      <ExclamationTriangleIcon className="mt-0.5 h-5 w-5 shrink-0 text-warn" aria-hidden="true" />
      <div className="min-w-0 space-y-2">
        {missing.length > 0 && (
          <>
            <p className="font-medium">Changes made here will be lost on the next container update.</p>
            <ul className="list-disc space-y-1 pl-5 text-ink">
              {missing.map((i) => (
                <li key={i.dir}>
                  <span className="font-mono">{i.path}</span> is not on a volume: {i.holds}.
                </li>
              ))}
            </ul>
          </>
        )}
        {legacy.map((i) => (
          <p key={i.dir} className="text-ink">
            <span className="font-mono">{i.path}</span> is mounted, but paperless-gpt has not read
            prompts from there since October 2024, so the prompts in it are ignored. Mount the
            folder at <span className="font-mono">{i.fix.split(":")[1]}</span> instead.
          </p>
        ))}
        <p className="text-muted">Put these volumes in your docker compose file and recreate the container:</p>
        <pre className="overflow-x-auto rounded bg-surface px-3 py-2 font-mono text-xs">
          {"volumes:\n" + [...new Set(shown.map((i) => i.fix))].map((f) => `  - ${f}`).join("\n")}
        </pre>
      </div>
    </div>
  );
};

export default PersistenceWarning;
