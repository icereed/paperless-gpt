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

  return (
    <div className="flex gap-3 rounded-md border border-warn bg-warn-tint px-4 py-3 text-sm" role="alert">
      <ExclamationTriangleIcon className="mt-0.5 h-5 w-5 shrink-0 text-warn" aria-hidden="true" />
      <div className="min-w-0 space-y-2">
        <p className="font-medium">Changes made here will be lost on the next container update.</p>
        <ul className="list-disc space-y-1 pl-5 text-ink">
          {shown.map((i) => (
            <li key={i.dir}>
              <span className="font-mono">{i.path}</span> is not on a volume: {i.holds}.
            </li>
          ))}
        </ul>
        <p className="text-muted">Add these volumes to your docker compose file and recreate the container:</p>
        <pre className="overflow-x-auto rounded bg-surface px-3 py-2 font-mono text-xs">
          {"volumes:\n" + [...new Set(shown.map((i) => i.fix))].map((f) => `  - ${f}`).join("\n")}
        </pre>
      </div>
    </div>
  );
};

export default PersistenceWarning;
