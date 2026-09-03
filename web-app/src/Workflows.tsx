import {
  PlusIcon,
  TrashIcon,
  PencilSquareIcon,
  CheckIcon,
  XMarkIcon,
  InformationCircleIcon,
} from "@heroicons/react/24/outline";
import axios from "axios";
import React, { useEffect, useState } from "react";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface WorkflowConfig {
  id: string;
  name: string;
  trigger_tag: string;
  completion_tag?: string;
  generate_titles?: boolean | null;
  generate_tags?: boolean | null;
  generate_correspondents?: boolean | null;
  generate_created_date?: boolean | null;
  generate_document_types?: boolean | null;
  generate_custom_fields?: boolean | null;
  prompts?: Record<string, string>;
}

const PROMPT_KEYS = [
  { key: "title_prompt", label: "Title prompt" },
  { key: "tag_prompt", label: "Tags prompt" },
  { key: "correspondent_prompt", label: "Correspondent prompt" },
  { key: "document_type_prompt", label: "Document type prompt" },
  { key: "date_prompt", label: "Created date prompt" },
  { key: "custom_field_prompt", label: "Custom fields prompt" },
];

const FLAG_KEYS: { key: keyof WorkflowConfig; label: string }[] = [
  { key: "generate_titles", label: "Generate title" },
  { key: "generate_tags", label: "Generate tags" },
  { key: "generate_correspondents", label: "Generate correspondent" },
  { key: "generate_document_types", label: "Generate document type" },
  { key: "generate_created_date", label: "Generate created date" },
  { key: "generate_custom_fields", label: "Generate custom fields" },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const emptyWorkflow = (): WorkflowConfig => ({
  id: "",
  name: "",
  trigger_tag: "",
  completion_tag: "",
  prompts: {},
});

/** Tri-state flag selector: null = inherit global, true = enable, false = disable */
const TriStateSelect: React.FC<{
  value: boolean | null | undefined;
  onChange: (v: boolean | null) => void;
}> = ({ value, onChange }) => (
  <select
    value={value === null || value === undefined ? "inherit" : String(value)}
    onChange={(e) => {
      const v = e.target.value;
      onChange(v === "inherit" ? null : v === "true");
    }}
    className="rounded border border-line bg-surface px-2 py-1 text-xs"
  >
    <option value="inherit">Inherit global</option>
    <option value="true">Enabled</option>
    <option value="false">Disabled</option>
  </select>
);

// ---------------------------------------------------------------------------
// WorkflowEditor
// ---------------------------------------------------------------------------

interface WorkflowEditorProps {
  initial: WorkflowConfig;
  onSave: (wf: WorkflowConfig) => void;
  onCancel: () => void;
  isNew: boolean;
}

const WorkflowEditor: React.FC<WorkflowEditorProps> = ({
  initial,
  onSave,
  onCancel,
  isNew,
}) => {
  const [wf, setWf] = useState<WorkflowConfig>({ ...initial, prompts: { ...(initial.prompts ?? {}) } });
  const [activePromptKey, setActivePromptKey] = useState<string>(PROMPT_KEYS[0].key);

  const setFlag = (key: keyof WorkflowConfig, value: boolean | null) => {
    setWf((prev) => ({ ...prev, [key]: value }));
  };

  return (
    <div className="space-y-6 rounded-lg border border-primary bg-surface p-6">
      {/* Basic fields */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label className="mb-1 block text-xs font-medium text-muted">
            Workflow name <span className="text-neg">*</span>
          </label>
          <input
            className="w-full rounded border border-line bg-surface px-3 py-1.5 text-sm"
            placeholder="e.g. Invoices"
            value={wf.name}
            onChange={(e) => setWf((p) => ({ ...p, name: e.target.value }))}
          />
        </div>
        <div>
          <label className="mb-1 block text-xs font-medium text-muted">
            Trigger tag <span className="text-neg">*</span>
          </label>
          <input
            className="w-full rounded border border-line bg-surface px-3 py-1.5 text-sm font-mono"
            placeholder="e.g. paperless-gpt-invoices"
            value={wf.trigger_tag}
            onChange={(e) => setWf((p) => ({ ...p, trigger_tag: e.target.value }))}
          />
          <p className="mt-1 text-xs text-faint">
            Tag in paperless-ngx that activates this workflow.
          </p>
        </div>
        <div>
          <label className="mb-1 block text-xs font-medium text-muted">
            Completion tag (optional)
          </label>
          <input
            className="w-full rounded border border-line bg-surface px-3 py-1.5 text-sm font-mono"
            placeholder="e.g. paperless-gpt-invoices-done"
            value={wf.completion_tag ?? ""}
            onChange={(e) => setWf((p) => ({ ...p, completion_tag: e.target.value }))}
          />
          <p className="mt-1 text-xs text-faint">
            Added to the document after successful processing.
          </p>
        </div>
        {!isNew && (
          <div>
            <label className="mb-1 block text-xs font-medium text-muted">
              Workflow ID
            </label>
            <input
              className="w-full rounded border border-line bg-surface-2 px-3 py-1.5 text-sm font-mono text-faint"
              value={wf.id}
              readOnly
            />
          </div>
        )}
      </div>

      {/* Generation flags */}
      <div>
        <h3 className="mb-3 text-sm font-semibold">Generation flags</h3>
        <div className="flex flex-wrap gap-4">
          {FLAG_KEYS.map(({ key, label }) => (
            <div key={key} className="flex flex-col gap-1">
              <span className="text-xs text-muted">{label}</span>
              <TriStateSelect
                value={wf[key] as boolean | null}
                onChange={(v) => setFlag(key, v)}
              />
            </div>
          ))}
        </div>
        <p className="mt-2 flex items-center gap-1 text-xs text-faint">
          <InformationCircleIcon className="h-4 w-4 shrink-0" />
          "Inherit global" uses the server's AUTO_GENERATE_* environment variables.
        </p>
      </div>

      {/* Per-workflow prompts */}
      <div>
        <h3 className="mb-3 text-sm font-semibold">Custom prompts</h3>
        <p className="mb-3 text-xs text-faint">
          Leave a prompt empty to use the global default from Settings → Prompts.
        </p>
        <div className="flex gap-2 flex-wrap mb-3">
          {PROMPT_KEYS.map(({ key, label }) => (
            <button
              key={key}
              type="button"
              onClick={() => setActivePromptKey(key)}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                activePromptKey === key
                  ? "bg-primary-tint text-ink"
                  : "bg-surface-2 text-muted hover:text-ink"
              } ${wf.prompts?.[key] ? "ring-1 ring-primary" : ""}`}
            >
              {label}
              {wf.prompts?.[key] ? " •" : ""}
            </button>
          ))}
        </div>
        <textarea
          className="w-full rounded border border-line bg-surface px-3 py-2 font-mono text-xs"
          rows={10}
          placeholder={`Leave empty to use the global ${activePromptKey} template…`}
          value={wf.prompts?.[activePromptKey] ?? ""}
          onChange={(e) =>
            setWf((p) => ({
              ...p,
              prompts: { ...(p.prompts ?? {}), [activePromptKey]: e.target.value },
            }))
          }
        />
      </div>

      {/* Actions */}
      <div className="flex justify-end gap-3">
        <button
          type="button"
          onClick={onCancel}
          className="inline-flex h-9 items-center gap-2 rounded-md border border-line px-4 text-sm text-muted hover:bg-surface-2"
        >
          <XMarkIcon className="h-4 w-4" /> Cancel
        </button>
        <button
          type="button"
          onClick={() => onSave(wf)}
          className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-white hover:bg-primary-dark"
        >
          <CheckIcon className="h-4 w-4" /> Save workflow
        </button>
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// WorkflowCard
// ---------------------------------------------------------------------------

const WorkflowCard: React.FC<{
  wf: WorkflowConfig;
  onEdit: () => void;
  onDelete: () => void;
}> = ({ wf, onEdit, onDelete }) => {
  const overriddenFlags = FLAG_KEYS.filter(({ key }) => wf[key] !== null && wf[key] !== undefined);
  const overriddenPrompts = Object.keys(wf.prompts ?? {}).filter(
    (k) => (wf.prompts ?? {})[k]?.trim()
  );

  return (
    <div className="rounded-lg border border-line bg-surface p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h3 className="truncate font-semibold">{wf.name || wf.id}</h3>
          <p className="mt-0.5 font-mono text-xs text-muted">
            Trigger: <span className="text-ink">{wf.trigger_tag}</span>
            {wf.completion_tag && (
              <>
                {" "}→{" "}
                <span className="text-ink">{wf.completion_tag}</span>
              </>
            )}
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          <button
            type="button"
            onClick={onEdit}
            className="rounded-md p-1.5 text-muted hover:bg-surface-2 hover:text-ink"
            title="Edit"
          >
            <PencilSquareIcon className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={onDelete}
            className="rounded-md p-1.5 text-muted hover:bg-surface-2 hover:text-neg"
            title="Delete"
          >
            <TrashIcon className="h-4 w-4" />
          </button>
        </div>
      </div>
      {(overriddenFlags.length > 0 || overriddenPrompts.length > 0) && (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {overriddenFlags.map(({ key, label }) => (
            <span
              key={key}
              className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
                wf[key] ? "bg-pos-tint text-pos-ink" : "bg-neg-tint text-neg-ink"
              }`}
            >
              {wf[key] ? "✓" : "✗"} {label}
            </span>
          ))}
          {overriddenPrompts.map((k) => (
            <span
              key={k}
              className="inline-flex items-center rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium text-ink"
            >
              Custom: {PROMPT_KEYS.find((p) => p.key === k)?.label ?? k}
            </span>
          ))}
        </div>
      )}
    </div>
  );
};

// ---------------------------------------------------------------------------
// Workflows page
// ---------------------------------------------------------------------------

const Workflows: React.FC = () => {
  const [workflows, setWorkflows] = useState<WorkflowConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null); // null = none, "new" = creating
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);

  const fetchWorkflows = async () => {
    try {
      const res = await axios.get<WorkflowConfig[]>("./api/workflows");
      setWorkflows(res.data ?? []);
    } catch (e: unknown) {
      setError((e as Error).message ?? "Failed to load workflows");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchWorkflows();
  }, []);

  const handleSave = async (wf: WorkflowConfig) => {
    try {
      if (editingId === "new") {
        const res = await axios.post<WorkflowConfig>("./api/workflows", wf);
        setWorkflows((prev) => [...prev, res.data]);
      } else {
        const res = await axios.put<WorkflowConfig>(`./api/workflows/${wf.id}`, wf);
        setWorkflows((prev) => prev.map((w) => (w.id === wf.id ? res.data : w)));
      }
      setEditingId(null);
    } catch (e: unknown) {
      const msg =
        (e as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        (e as Error).message ??
        "Failed to save workflow";
      setError(msg);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await axios.delete(`./api/workflows/${id}`);
      setWorkflows((prev) => prev.filter((w) => w.id !== id));
      setDeleteConfirmId(null);
    } catch (e: unknown) {
      setError((e as Error).message ?? "Failed to delete workflow");
    }
  };

  const editingWorkflow =
    editingId === "new"
      ? emptyWorkflow()
      : workflows.find((w) => w.id === editingId) ?? null;

  return (
    <div className="mx-auto max-w-5xl space-y-8 px-4 py-8 sm:px-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Workflows</h1>
          <p className="mt-1 text-sm text-muted">
            Each workflow watches a specific paperless-ngx tag and applies its own
            prompts and generation settings.
          </p>
        </div>
        {editingId === null && (
          <button
            type="button"
            onClick={() => setEditingId("new")}
            className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-white hover:bg-primary-dark"
          >
            <PlusIcon className="h-4 w-4" /> New workflow
          </button>
        )}
      </div>

      {error && (
        <div className="rounded-md border border-neg-line bg-neg-tint px-4 py-3 text-sm text-neg-ink">
          {error}
          <button
            type="button"
            className="ml-2 underline"
            onClick={() => setError(null)}
          >
            Dismiss
          </button>
        </div>
      )}

      {/* New / edit form */}
      {editingId !== null && editingWorkflow !== null && (
        <WorkflowEditor
          key={editingId}
          initial={editingWorkflow}
          isNew={editingId === "new"}
          onSave={handleSave}
          onCancel={() => setEditingId(null)}
        />
      )}

      {/* List */}
      {loading ? (
        <p className="text-sm text-muted">Loading…</p>
      ) : workflows.length === 0 && editingId === null ? (
        <div className="rounded-lg border border-dashed border-line p-10 text-center">
          <p className="text-sm text-muted">No workflows configured yet.</p>
          <p className="mt-1 text-xs text-faint">
            Create one to process documents with a custom prompt and settings.
          </p>
        </div>
      ) : (
        <div className="space-y-4">
          {workflows.map((wf) =>
            editingId === wf.id ? null : (
              <div key={wf.id}>
                {deleteConfirmId === wf.id ? (
                  <div className="rounded-lg border border-neg-line bg-neg-tint p-4 text-sm">
                    <p className="font-medium text-neg-ink">
                      Delete workflow "{wf.name || wf.id}"?
                    </p>
                    <p className="mt-1 text-xs text-faint">
                      Documents tagged with{" "}
                      <span className="font-mono">{wf.trigger_tag}</span> will fall
                      back to the global AUTO_TAG behaviour (if that tag is also
                      configured).
                    </p>
                    <div className="mt-3 flex gap-2">
                      <button
                        type="button"
                        onClick={() => handleDelete(wf.id)}
                        className="inline-flex h-8 items-center gap-1 rounded-md bg-neg px-3 text-xs font-medium text-white hover:bg-neg-dark"
                      >
                        <TrashIcon className="h-3.5 w-3.5" /> Delete
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeleteConfirmId(null)}
                        className="inline-flex h-8 items-center rounded-md border border-line px-3 text-xs text-muted hover:bg-surface-2"
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                ) : (
                  <WorkflowCard
                    wf={wf}
                    onEdit={() => {
                      setEditingId(wf.id);
                      setDeleteConfirmId(null);
                    }}
                    onDelete={() => {
                      setDeleteConfirmId(wf.id);
                      setEditingId(null);
                    }}
                  />
                )}
              </div>
            )
          )}
        </div>
      )}
    </div>
  );
};

export default Workflows;
