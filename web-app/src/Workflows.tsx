import {
  ArrowPathIcon,
  BeakerIcon,
  CheckIcon,
  DocumentDuplicateIcon,
  ExclamationTriangleIcon,
  FolderIcon,
  InformationCircleIcon,
  MagnifyingGlassIcon,
  PencilSquareIcon,
  PlusIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import axios from "axios";
import classNames from "classnames";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Button from "./components/ui/Button";
import Toast, { ToastData } from "./components/ui/Toast";

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
  enable_ocr?: boolean | null;
  ocr_limit_pages?: number | null;
  prompts?: Record<string, string>;
}

type FlagKey =
  | "generate_titles"
  | "generate_tags"
  | "generate_correspondents"
  | "generate_document_types"
  | "generate_created_date"
  | "generate_custom_fields";

/** What a workflow inherits when it does not override a setting. */
interface WorkflowDefaults {
  auto_tag: string;
  auto_tag_complete: string;
  storage_dir: string;
  ocr_enabled: boolean;
  ocr_prompt_override_supported: boolean;
  generate: Record<FlagKey, boolean>;
  prompts: Record<string, string>;
}

interface WaitingDocument {
  id: number;
  title: string;
}

interface CustomFieldSuggestion {
  id: number;
  name: string;
  value: unknown;
}

interface PreviewResult {
  suggestion: {
    original_document: { id: number; title: string; correspondent?: string; document_type_name?: string; created_date?: string };
    suggested_title?: string;
    suggested_tags?: string[];
    suggested_correspondent?: string;
    suggested_document_type?: string;
    suggested_created_date?: string;
    suggested_custom_fields?: CustomFieldSuggestion[];
  };
  steps: Record<FlagKey, boolean>;
  ocr_skipped: boolean;
}

const PROMPT_KEYS: { key: string; label: string; file: string }[] = [
  { key: "title_prompt", label: "Title", file: "title_prompt.tmpl" },
  { key: "tag_prompt", label: "Tags", file: "tag_prompt.tmpl" },
  { key: "correspondent_prompt", label: "Correspondent", file: "correspondent_prompt.tmpl" },
  { key: "document_type_prompt", label: "Document type", file: "document_type_prompt.tmpl" },
  { key: "date_prompt", label: "Created date", file: "created_date_prompt.tmpl" },
  { key: "custom_field_prompt", label: "Custom fields", file: "custom_field_prompt.tmpl" },
  { key: "ocr_prompt", label: "OCR", file: "ocr_prompt.tmpl" },
];

const FLAG_KEYS: { key: FlagKey; label: string }[] = [
  { key: "generate_titles", label: "Title" },
  { key: "generate_tags", label: "Tags" },
  { key: "generate_correspondents", label: "Correspondent" },
  { key: "generate_document_types", label: "Document type" },
  { key: "generate_created_date", label: "Created date" },
  { key: "generate_custom_fields", label: "Custom fields" },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const emptyWorkflow = (): WorkflowConfig => ({
  id: "",
  name: "",
  trigger_tag: "",
  completion_tag: "",
  enable_ocr: false,
  prompts: {},
});

const customPromptKeys = (wf: WorkflowConfig): string[] =>
  Object.entries(wf.prompts ?? {})
    .filter(([, v]) => !!v?.trim())
    .map(([k]) => k);

/** Copy a workflow for create-from-duplicate; a trigger tag can only be used once. */
const cloneWorkflowForDuplicate = (wf: WorkflowConfig): WorkflowConfig => ({
  ...wf,
  id: "",
  name: wf.name ? `${wf.name} (copy)` : "",
  trigger_tag: "",
  completion_tag: "",
  prompts: { ...(wf.prompts ?? {}) },
});

const apiError = (e: unknown, fallback: string): string =>
  (e as { response?: { data?: { error?: string } } })?.response?.data?.error ??
  (e as Error)?.message ??
  fallback;

const sameTag = (a?: string, b?: string) =>
  !!a && !!b && a.trim().toLowerCase() === b.trim().toLowerCase();

/** The template variables a prompt uses, e.g. ".Content", so users know what they can use. */
const templateVariables = (template: string): string[] => {
  const found = new Set<string>();
  for (const match of template.matchAll(/\{\{-?\s*[^}]*?(\.[A-Z][A-Za-z]*)/g)) {
    found.add(match[1]);
  }
  return [...found].sort();
};

const formatValue = (value: unknown): string => {
  if (value === null || value === undefined || value === "") return "—";
  if (Array.isArray(value)) return value.join(", ");
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
};

const inputClass =
  "w-full rounded-md border border-line bg-surface px-3 py-1.5 text-sm focus:border-primary focus:outline-none";

// ---------------------------------------------------------------------------
// Small building blocks
// ---------------------------------------------------------------------------

const Section: React.FC<{ title: string; hint?: React.ReactNode; children: React.ReactNode }> = ({
  title,
  hint,
  children,
}) => (
  <section className="space-y-3">
    <div>
      <h3 className="text-sm font-semibold">{title}</h3>
      {hint && <p className="mt-0.5 text-xs text-muted">{hint}</p>}
    </div>
    {children}
  </section>
);

const FieldError: React.FC<{ message?: string }> = ({ message }) =>
  message ? (
    <p className="mt-1 flex items-center gap-1 text-xs text-neg">
      <ExclamationTriangleIcon className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      {message}
    </p>
  ) : null;

/** Inherit / On / Off, showing what "inherit" currently means. */
const StepToggle: React.FC<{
  label: string;
  value: boolean | null | undefined;
  inherited: boolean | undefined;
  onChange: (v: boolean | null) => void;
}> = ({ label, value, inherited, onChange }) => {
  const options: { v: boolean | null; text: string }[] = [
    { v: null, text: inherited === undefined ? "Default" : `Default (${inherited ? "on" : "off"})` },
    { v: true, text: "On" },
    { v: false, text: "Off" },
  ];
  const current = value === undefined ? null : value;
  const effective = current === null ? inherited : current;
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border border-line px-3 py-2">
      <span className={classNames("text-sm", effective === false && "text-faint line-through")}>
        {label}
      </span>
      <div className="flex rounded-md bg-surface-2 p-0.5" role="radiogroup" aria-label={label}>
        {options.map((o) => (
          <button
            key={String(o.v)}
            type="button"
            role="radio"
            aria-checked={current === o.v}
            onClick={() => onChange(o.v)}
            className={classNames(
              "rounded px-2 py-0.5 text-xs transition-colors",
              current === o.v ? "bg-surface font-medium text-ink shadow-card" : "text-muted hover:text-ink"
            )}
          >
            {o.text}
          </button>
        ))}
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Try-it panel
// ---------------------------------------------------------------------------

const ResultRow: React.FC<{ label: string; before?: string; after: string }> = ({
  label,
  before,
  after,
}) => (
  <div className="grid grid-cols-[8rem_1fr] gap-3 border-t border-line py-2 text-sm first:border-t-0">
    <span className="text-muted">{label}</span>
    <div className="min-w-0">
      <span className="break-words font-medium">{after || "—"}</span>
      {before !== undefined && before !== after && (
        <span className="mt-0.5 block break-words text-xs text-faint">was: {before || "—"}</span>
      )}
    </div>
  </div>
);

const TryIt: React.FC<{ workflow: WorkflowConfig; savedId?: string }> = ({ workflow, savedId }) => {
  const [waiting, setWaiting] = useState<WaitingDocument[] | null>(null);
  const [documentId, setDocumentId] = useState("");
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<PreviewResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!savedId) return;
    axios
      .get<{ documents: WaitingDocument[] }>(`./api/workflows/${savedId}/documents`)
      .then((res) => {
        setWaiting(res.data.documents);
        if (res.data.documents.length > 0) setDocumentId(String(res.data.documents[0].id));
      })
      .catch(() => setWaiting(null));
  }, [savedId]);

  const run = async () => {
    setRunning(true);
    setError(null);
    setResult(null);
    try {
      const res = await axios.post<PreviewResult>("./api/workflows/preview", {
        workflow,
        document_id: Number(documentId),
      });
      setResult(res.data);
    } catch (e) {
      setError(apiError(e, "The test run failed"));
    } finally {
      setRunning(false);
    }
  };

  const s = result?.suggestion;
  const before = s?.original_document;

  return (
    <div className="space-y-3 rounded-lg border border-line bg-surface-2 p-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-0 flex-1">
          <label htmlFor="try-doc" className="mb-1 block text-xs font-medium text-muted">
            Document
          </label>
          {waiting && waiting.length > 0 ? (
            <select
              id="try-doc"
              className={inputClass}
              value={documentId}
              onChange={(e) => setDocumentId(e.target.value)}
            >
              {waiting.map((d) => (
                <option key={d.id} value={d.id}>
                  #{d.id} {d.title}
                </option>
              ))}
            </select>
          ) : (
            <input
              id="try-doc"
              type="number"
              min={1}
              className={classNames(inputClass, "font-mono")}
              placeholder="Document ID, e.g. 123"
              value={documentId}
              onChange={(e) => setDocumentId(e.target.value)}
            />
          )}
        </div>
        <Button
          variant="primary"
          onClick={run}
          loading={running}
          disabled={!documentId || Number(documentId) <= 0}
        >
          <BeakerIcon className="h-4 w-4" aria-hidden="true" /> Run test
        </Button>
      </div>
      <p className="text-xs text-muted">
        Runs this workflow as it is in the editor, unsaved changes included. Nothing is written to
        paperless-ngx.
        {waiting && waiting.length > 0 && " The list shows documents waiting for this workflow."}
      </p>

      {error && (
        <p className="rounded-md bg-neg-tint px-3 py-2 text-sm text-neg" role="alert">
          {error}
        </p>
      )}

      {result && s && before && (
        <div className="rounded-md border border-line bg-surface p-3">
          {result.steps.generate_titles && (
            <ResultRow label="Title" before={before.title} after={s.suggested_title ?? ""} />
          )}
          {result.steps.generate_tags && (
            <ResultRow label="Tags" after={(s.suggested_tags ?? []).join(", ")} />
          )}
          {result.steps.generate_correspondents && (
            <ResultRow
              label="Correspondent"
              before={before.correspondent}
              after={s.suggested_correspondent ?? ""}
            />
          )}
          {result.steps.generate_document_types && (
            <ResultRow
              label="Document type"
              before={before.document_type_name}
              after={s.suggested_document_type ?? ""}
            />
          )}
          {result.steps.generate_created_date && (
            <ResultRow
              label="Created date"
              before={before.created_date}
              after={s.suggested_created_date ?? ""}
            />
          )}
          {result.steps.generate_custom_fields &&
            (s.suggested_custom_fields ?? []).map((f) => (
              <ResultRow key={f.id} label={f.name} after={formatValue(f.value)} />
            ))}
          {Object.values(result.steps).every((on) => !on) && (
            <p className="text-sm text-muted">All steps are off for this workflow.</p>
          )}
          {result.ocr_skipped && (
            <p className="mt-2 text-xs text-muted">
              OCR is not run in a test; the document&apos;s current text was used.
            </p>
          )}
        </div>
      )}
    </div>
  );
};

// ---------------------------------------------------------------------------
// WorkflowEditor
// ---------------------------------------------------------------------------

interface WorkflowEditorProps {
  initial: WorkflowConfig;
  isNew: boolean;
  defaults: WorkflowDefaults | null;
  tagNames: string[];
  otherWorkflows: WorkflowConfig[];
  saving: boolean;
  onSave: (wf: WorkflowConfig) => void;
  onCancel: () => void;
}

const WorkflowEditor: React.FC<WorkflowEditorProps> = ({
  initial,
  isNew,
  defaults,
  tagNames,
  otherWorkflows,
  saving,
  onSave,
  onCancel,
}) => {
  const [wf, setWf] = useState<WorkflowConfig>({
    ...initial,
    prompts: { ...(initial.prompts ?? {}) },
  });
  const [activePrompt, setActivePrompt] = useState(PROMPT_KEYS[0].key);
  const [showTryIt, setShowTryIt] = useState(false);

  const set = <K extends keyof WorkflowConfig>(key: K, value: WorkflowConfig[K]) =>
    setWf((prev) => ({ ...prev, [key]: value }));
  const setPrompt = (key: string, value: string) =>
    setWf((prev) => ({ ...prev, prompts: { ...(prev.prompts ?? {}), [key]: value } }));

  // Instant feedback for the mistakes the server would reject anyway.
  const triggerError = (() => {
    if (!wf.trigger_tag.trim()) return undefined;
    if (defaults && sameTag(wf.trigger_tag, defaults.auto_tag))
      return `This is AUTO_TAG, which already runs the default processing.`;
    if (defaults && sameTag(wf.trigger_tag, defaults.auto_tag_complete))
      return `This is AUTO_TAG_COMPLETE, which paperless-gpt adds after processing.`;
    const clash = otherWorkflows.find((o) => sameTag(o.trigger_tag, wf.trigger_tag));
    if (clash) return `Already the trigger of "${clash.name || clash.id}".`;
    const chained = otherWorkflows.find((o) => sameTag(o.completion_tag, wf.trigger_tag));
    if (chained) return `This is the completion tag of "${chained.name || chained.id}"; workflows can't be chained.`;
    return undefined;
  })();
  const completionError = (() => {
    if (!wf.completion_tag?.trim()) return undefined;
    if (sameTag(wf.completion_tag, wf.trigger_tag))
      return "Must differ from the trigger tag, or documents are processed again and again.";
    if (defaults && sameTag(wf.completion_tag, defaults.auto_tag))
      return "This is AUTO_TAG; documents would be processed again.";
    const chained = otherWorkflows.find((o) => sameTag(o.trigger_tag, wf.completion_tag));
    if (chained) return `This is the trigger of "${chained.name || chained.id}"; workflows can't be chained.`;
    return undefined;
  })();
  const canSave = !!wf.trigger_tag.trim() && !triggerError && !completionError;

  const tagExists = (name?: string) => !!name?.trim() && tagNames.some((t) => sameTag(t, name));
  const newTagHint = (name?: string) =>
    name?.trim() && tagNames.length > 0 && !tagExists(name)
      ? "New tag — it will be created in paperless-ngx when you save."
      : undefined;

  const visiblePrompts = PROMPT_KEYS.filter((p) => p.key !== "ocr_prompt" || wf.enable_ocr);
  const active = visiblePrompts.find((p) => p.key === activePrompt) ?? visiblePrompts[0];
  const activeValue = wf.prompts?.[active.key] ?? "";
  const globalPrompt = defaults?.prompts[active.key] ?? "";
  const variables = templateVariables(activeValue.trim() ? activeValue : globalPrompt);

  return (
    <div className="space-y-8 rounded-lg border border-primary bg-surface p-6 shadow-card">
      <datalist id="paperless-tags">
        {tagNames.map((t) => (
          <option key={t} value={t} />
        ))}
      </datalist>

      <Section title="When">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label htmlFor="wf-name" className="mb-1 block text-xs font-medium text-muted">
              Name
            </label>
            <input
              id="wf-name"
              className={inputClass}
              placeholder="e.g. Invoices"
              value={wf.name}
              onChange={(e) => set("name", e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="wf-trigger" className="mb-1 block text-xs font-medium text-muted">
              Trigger tag <span className="text-neg">*</span>
            </label>
            <input
              id="wf-trigger"
              list="paperless-tags"
              className={classNames(inputClass, "font-mono", triggerError && "border-neg")}
              placeholder="e.g. paperless-gpt-invoices"
              value={wf.trigger_tag}
              onChange={(e) => set("trigger_tag", e.target.value)}
            />
            {triggerError ? (
              <FieldError message={triggerError} />
            ) : (
              <p className="mt-1 text-xs text-muted">
                {newTagHint(wf.trigger_tag) ??
                  "Documents with this tag are processed by this workflow."}
              </p>
            )}
          </div>
          <div>
            <label htmlFor="wf-complete" className="mb-1 block text-xs font-medium text-muted">
              Completion tag
            </label>
            <input
              id="wf-complete"
              list="paperless-tags"
              className={classNames(inputClass, "font-mono", completionError && "border-neg")}
              placeholder={defaults?.auto_tag_complete || "optional"}
              value={wf.completion_tag ?? ""}
              onChange={(e) => set("completion_tag", e.target.value)}
            />
            {completionError ? (
              <FieldError message={completionError} />
            ) : (
              <p className="mt-1 text-xs text-muted">
                {newTagHint(wf.completion_tag) ??
                  (defaults?.auto_tag_complete
                    ? `Added when done. Empty: ${defaults.auto_tag_complete} (AUTO_TAG_COMPLETE).`
                    : "Added when done. Optional.")}
              </p>
            )}
          </div>
          {!isNew && defaults && (
            <div>
              <span className="mb-1 block text-xs font-medium text-muted">Stored in</span>
              <p className="flex items-center gap-1.5 py-1.5 font-mono text-xs text-muted">
                <FolderIcon className="h-4 w-4 shrink-0" aria-hidden="true" />
                {defaults.storage_dir}/{wf.id}/
              </p>
            </div>
          )}
        </div>
      </Section>

      <Section
        title="What it does"
        hint="Default follows the AUTO_GENERATE_* settings, like the AUTO_TAG processing."
      >
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {FLAG_KEYS.map(({ key, label }) => (
            <StepToggle
              key={key}
              label={label}
              value={wf[key]}
              inherited={defaults?.generate[key]}
              onChange={(v) => set(key, v)}
            />
          ))}
        </div>
        <div className="rounded-md border border-line px-3 py-2">
          <label className="flex items-center justify-between gap-3">
            <span className="text-sm">
              Run OCR first
              {defaults && !defaults.ocr_enabled && (
                <span className="ml-2 text-xs text-muted">(no OCR provider configured)</span>
              )}
            </span>
            <input
              type="checkbox"
              className="h-4 w-4 accent-[var(--color-primary)]"
              checked={!!wf.enable_ocr}
              disabled={!!defaults && !defaults.ocr_enabled && !wf.enable_ocr}
              onChange={(e) => set("enable_ocr", e.target.checked)}
            />
          </label>
          {wf.enable_ocr && (
            <div className="mt-2 flex items-center gap-2 text-xs text-muted">
              <label htmlFor="wf-pages">Page limit</label>
              <input
                id="wf-pages"
                type="number"
                min={0}
                className="h-7 w-20 rounded border border-line bg-surface px-2 font-mono text-xs"
                placeholder="default"
                value={wf.ocr_limit_pages ?? ""}
                onChange={(e) =>
                  set("ocr_limit_pages", e.target.value === "" ? null : Number(e.target.value))
                }
              />
              <span>0 = all pages, empty = OCR default</span>
            </div>
          )}
        </div>
      </Section>

      <Section
        title="Prompts"
        hint="Only change what differs. A prompt you leave empty uses the global one from Settings."
      >
        <div className="flex flex-wrap gap-1.5" role="tablist">
          {visiblePrompts.map(({ key, label }) => {
            const custom = !!wf.prompts?.[key]?.trim();
            return (
              <button
                key={key}
                type="button"
                role="tab"
                aria-selected={active.key === key}
                onClick={() => setActivePrompt(key)}
                className={classNames(
                  "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                  active.key === key ? "bg-primary-tint text-ink" : "bg-surface-2 text-muted hover:text-ink"
                )}
              >
                {label}
                {custom && (
                  <span className="ml-1.5 inline-block h-1.5 w-1.5 rounded-full bg-primary align-middle" />
                )}
              </button>
            );
          })}
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-muted">
            {activeValue.trim() ? (
              <>
                Custom prompt, saved as <span className="font-mono">{active.file}</span>
              </>
            ) : (
              "Using the global prompt (shown greyed out below)."
            )}
          </p>
          {activeValue.trim() ? (
            <Button size="sm" variant="ghost" onClick={() => setPrompt(active.key, "")}>
              <ArrowPathIcon className="h-4 w-4" aria-hidden="true" /> Use global prompt
            </Button>
          ) : (
            globalPrompt && (
              <Button size="sm" variant="secondary" onClick={() => setPrompt(active.key, globalPrompt)}>
                <PencilSquareIcon className="h-4 w-4" aria-hidden="true" /> Customize
              </Button>
            )
          )}
        </div>
        {active.key === "ocr_prompt" && defaults && !defaults.ocr_prompt_override_supported && (
          <p className="text-xs text-warn">Only the LLM OCR provider can use a custom OCR prompt.</p>
        )}
        <textarea
          aria-label={`${active.label} prompt`}
          className="w-full rounded-md border border-line bg-surface px-3 py-2 font-mono text-xs leading-relaxed placeholder:text-faint focus:border-primary focus:outline-none"
          rows={12}
          placeholder={globalPrompt || "Leave empty to use the global prompt."}
          value={activeValue}
          onChange={(e) => setPrompt(active.key, e.target.value)}
        />
        {variables.length > 0 && (
          <p className="flex flex-wrap items-center gap-1 text-xs text-muted">
            Variables:
            {variables.map((v) => (
              <code key={v} className="rounded bg-surface-2 px-1.5 py-0.5 font-mono">
                {`{{${v}}}`}
              </code>
            ))}
          </p>
        )}
      </Section>

      <Section title="Try it" hint="See what this workflow does to a real document before you save it.">
        {showTryIt ? (
          <TryIt workflow={wf} savedId={isNew ? undefined : initial.id} />
        ) : (
          <Button variant="secondary" onClick={() => setShowTryIt(true)}>
            <BeakerIcon className="h-4 w-4" aria-hidden="true" /> Test on a document
          </Button>
        )}
      </Section>

      <div className="flex justify-end gap-3 border-t border-line pt-4">
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button variant="primary" onClick={() => onSave(wf)} disabled={!canSave} loading={saving}>
          <CheckIcon className="h-4 w-4" aria-hidden="true" /> {isNew ? "Create workflow" : "Save changes"}
        </Button>
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Cards
// ---------------------------------------------------------------------------

const TagChip: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <span className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-ink">{children}</span>
);

const WaitingBadge: React.FC<{ id: string }> = ({ id }) => {
  const [state, setState] = useState<{ count: number; more: boolean } | null>(null);
  useEffect(() => {
    axios
      .get<{ documents: WaitingDocument[]; more: boolean }>(`./api/workflows/${id}/documents`)
      .then((res) => setState({ count: res.data.documents.length, more: res.data.more }))
      .catch(() => setState(null));
  }, [id]);
  if (!state) return null;
  if (state.count === 0) return <span className="text-xs text-faint">Nothing waiting</span>;
  return (
    <span className="rounded-full bg-warn-tint px-2 py-0.5 text-xs font-medium text-ink">
      {state.count}
      {state.more ? "+" : ""} waiting
    </span>
  );
};

const WorkflowCard: React.FC<{
  wf: WorkflowConfig;
  defaults: WorkflowDefaults | null;
  onEdit: () => void;
  onDuplicate: () => void;
  onDelete: () => void;
}> = ({ wf, defaults, onEdit, onDuplicate, onDelete }) => {
  const steps = FLAG_KEYS.filter(({ key }) => {
    const v = wf[key];
    return v === null || v === undefined ? defaults?.generate[key] ?? true : v;
  });
  const prompts = customPromptKeys(wf);
  const completion = wf.completion_tag || defaults?.auto_tag_complete;

  return (
    <div className="rounded-lg border border-line bg-surface p-5 shadow-card">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate font-semibold">{wf.name || wf.id}</h3>
            <WaitingBadge id={wf.id} />
          </div>
          <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted">
            <TagChip>{wf.trigger_tag}</TagChip>
            <span aria-hidden="true">→</span>
            {wf.enable_ocr && <span>OCR,</span>}
            <span>{steps.length > 0 ? steps.map((s) => s.label.toLowerCase()).join(", ") : "nothing"}</span>
            {completion && (
              <>
                <span aria-hidden="true">→</span>
                <TagChip>{completion}</TagChip>
              </>
            )}
          </p>
          <p className="text-xs text-muted">
            {prompts.length > 0
              ? `Own prompts: ${prompts.map((k) => PROMPT_KEYS.find((p) => p.key === k)?.label ?? k).join(", ")}`
              : "Uses the global prompts"}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          <Button size="sm" variant="ghost" onClick={onEdit} aria-label={`Edit ${wf.name || wf.id}`} title="Edit">
            <PencilSquareIcon className="h-4 w-4" aria-hidden="true" />
          </Button>
          <Button size="sm" variant="ghost" onClick={onDuplicate} aria-label={`Duplicate ${wf.name || wf.id}`} title="Duplicate">
            <DocumentDuplicateIcon className="h-4 w-4" aria-hidden="true" />
          </Button>
          <Button size="sm" variant="ghost" onClick={onDelete} aria-label={`Delete ${wf.name || wf.id}`} title="Delete">
            <TrashIcon className="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </div>
  );
};

/** The AUTO_TAG path, shown so the list reads as "these are all the ways documents get processed". */
const DefaultCard: React.FC<{ defaults: WorkflowDefaults }> = ({ defaults }) => {
  const steps = FLAG_KEYS.filter(({ key }) => defaults.generate[key]);
  return (
    <div className="rounded-lg border border-dashed border-line p-5">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-semibold">Default</h3>
        <span className="text-xs text-muted">AUTO_TAG · global prompts from Settings</span>
      </div>
      <p className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-muted">
        <TagChip>{defaults.auto_tag}</TagChip>
        <span aria-hidden="true">→</span>
        <span>{steps.map((s) => s.label.toLowerCase()).join(", ") || "nothing"}</span>
        {defaults.auto_tag_complete && (
          <>
            <span aria-hidden="true">→</span>
            <TagChip>{defaults.auto_tag_complete}</TagChip>
          </>
        )}
      </p>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Workflows page
// ---------------------------------------------------------------------------

const Workflows: React.FC = () => {
  const [workflows, setWorkflows] = useState<WorkflowConfig[]>([]);
  const [defaults, setDefaults] = useState<WorkflowDefaults | null>(null);
  const [tagNames, setTagNames] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastData | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null); // null = none, "new" = creating
  const [draft, setDraft] = useState<WorkflowConfig | null>(null);
  const [editorKey, setEditorKey] = useState(0);
  const [saving, setSaving] = useState(false);
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const editorRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    axios
      .get<WorkflowConfig[]>("./api/workflows")
      .then((res) => setWorkflows(res.data ?? []))
      .catch((e) => setError(apiError(e, "Failed to load workflows")))
      .finally(() => setLoading(false));
    axios
      .get<WorkflowDefaults>("./api/workflows/defaults")
      .then((res) => setDefaults(res.data))
      .catch(() => setDefaults(null));
    axios
      .get<Record<string, number>>("./api/tags")
      .then((res) => setTagNames(Object.keys(res.data ?? {}).sort((a, b) => a.localeCompare(b))))
      .catch(() => setTagNames([]));
  }, []);

  // Keep the editor in view inside the scrollable <main> pane.
  useEffect(() => {
    if (!editingId) return;
    const node = editorRef.current;
    if (!node) return;
    const id = requestAnimationFrame(() => node.scrollIntoView({ behavior: "smooth", block: "start" }));
    return () => cancelAnimationFrame(id);
  }, [editingId, editorKey]);

  const openEditor = (id: string, initial: WorkflowConfig | null = null) => {
    setDraft(initial);
    setEditorKey((k) => k + 1);
    setEditingId(id);
    setDeleteConfirmId(null);
    setError(null);
  };
  const closeEditor = () => {
    setEditingId(null);
    setDraft(null);
  };
  const dismissToast = useCallback(() => setToast(null), []);

  const handleSave = async (edited: WorkflowConfig) => {
    // A prompt copied from the global one and left unchanged is no override;
    // saving it would hide later changes to the global prompt.
    const prompts = Object.fromEntries(
      Object.entries(edited.prompts ?? {}).filter(
        ([key, value]) => value.trim() && value.trim() !== (defaults?.prompts[key] ?? "").trim()
      )
    );
    const wf = { ...edited, prompts };
    setSaving(true);
    setError(null);
    try {
      if (editingId === "new") {
        const res = await axios.post<WorkflowConfig>("./api/workflows", wf);
        setWorkflows((prev) => [...prev, res.data].sort((a, b) => a.id.localeCompare(b.id)));
        setToast({
          kind: "success",
          message: `Workflow created. Tag a document with "${res.data.trigger_tag}" to run it.`,
        });
      } else {
        const res = await axios.put<WorkflowConfig>(`./api/workflows/${wf.id}`, wf);
        setWorkflows((prev) => prev.map((w) => (w.id === wf.id ? res.data : w)));
        setToast({ kind: "success", message: "Workflow saved." });
      }
      setTagNames((prev) =>
        [...new Set([...prev, wf.trigger_tag, wf.completion_tag ?? ""].filter(Boolean))].sort((a, b) =>
          a.localeCompare(b)
        )
      );
      closeEditor();
    } catch (e) {
      setError(apiError(e, "Failed to save workflow"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await axios.delete(`./api/workflows/${id}`);
      setWorkflows((prev) => prev.filter((w) => w.id !== id));
      setDeleteConfirmId(null);
    } catch (e) {
      setError(apiError(e, "Failed to delete workflow"));
    }
  };

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return workflows;
    return workflows.filter((wf) =>
      `${wf.name} ${wf.trigger_tag} ${wf.completion_tag ?? ""} ${wf.id}`.toLowerCase().includes(q)
    );
  }, [workflows, query]);

  const editor = (initial: WorkflowConfig, isNew: boolean) => (
    <div ref={editorRef} className="scroll-mt-4">
      <WorkflowEditor
        key={editorKey}
        initial={initial}
        isNew={isNew}
        defaults={defaults}
        tagNames={tagNames}
        otherWorkflows={workflows.filter((w) => w.id !== initial.id)}
        saving={saving}
        onSave={handleSave}
        onCancel={closeEditor}
      />
    </div>
  );

  return (
    <div className="mx-auto max-w-4xl space-y-6 px-4 py-8 sm:px-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-2xl">
          <h1 className="text-xl font-semibold">AI Workflows</h1>
          <p className="mt-1 text-sm text-muted">
            Give each kind of document its own prompts and steps. Tag a document with a
            workflow&apos;s trigger tag (by hand, or with a paperless-ngx workflow or mail rule) and
            paperless-gpt processes it that way, then swaps the trigger for the completion tag.
          </p>
        </div>
        {editingId === null && (
          <Button variant="primary" onClick={() => openEditor("new", emptyWorkflow())}>
            <PlusIcon className="h-4 w-4" aria-hidden="true" /> New workflow
          </Button>
        )}
      </div>

      {error && (
        <div
          className="flex items-start justify-between gap-3 rounded-md bg-neg-tint px-4 py-3 text-sm text-neg"
          role="alert"
        >
          <span>{error}</span>
          <button type="button" className="shrink-0 underline" onClick={() => setError(null)}>
            Dismiss
          </button>
        </div>
      )}

      {editingId === "new" && editor(draft ?? emptyWorkflow(), true)}

      {!loading && workflows.length > 3 && editingId !== "new" && (
        <div className="relative">
          <MagnifyingGlassIcon
            className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-faint"
            aria-hidden="true"
          />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter by name or tag…"
            aria-label="Filter workflows"
            className={classNames(inputClass, "h-9 pl-9")}
          />
        </div>
      )}

      {loading ? (
        <p className="text-sm text-muted">Loading…</p>
      ) : (
        <div className="space-y-3">
          {workflows.length === 0 && editingId === null && (
            <div className="rounded-lg border border-line bg-surface p-8 text-center shadow-card">
              <p className="font-medium">No workflows yet</p>
              <p className="mx-auto mt-1 max-w-md text-sm text-muted">
                For example: an <em>Invoices</em> workflow whose title prompt asks for vendor and
                invoice number, or a <em>Private mail</em> workflow that only sets a title.
              </p>
              <Button className="mt-4" variant="primary" onClick={() => openEditor("new", emptyWorkflow())}>
                <PlusIcon className="h-4 w-4" aria-hidden="true" /> Create your first workflow
              </Button>
            </div>
          )}

          {filtered.map((wf) =>
            editingId === wf.id ? (
              <React.Fragment key={wf.id}>{editor(wf, false)}</React.Fragment>
            ) : deleteConfirmId === wf.id ? (
              <div key={wf.id} className="rounded-lg border border-neg bg-neg-tint p-4 text-sm">
                <p className="font-medium">Delete &ldquo;{wf.name || wf.id}&rdquo;?</p>
                <p className="mt-1 text-xs text-muted">
                  Documents tagged <span className="font-mono">{wf.trigger_tag}</span> are no longer
                  processed automatically. The workflow&apos;s files in{" "}
                  <span className="font-mono">{defaults?.storage_dir ?? "prompts/workflows"}/{wf.id}/</span>{" "}
                  are deleted.
                </p>
                <div className="mt-3 flex gap-2">
                  <Button size="sm" variant="danger" onClick={() => handleDelete(wf.id)}>
                    <TrashIcon className="h-4 w-4" aria-hidden="true" /> Delete
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setDeleteConfirmId(null)}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <WorkflowCard
                key={wf.id}
                wf={wf}
                defaults={defaults}
                onEdit={() => openEditor(wf.id)}
                onDuplicate={() => openEditor("new", cloneWorkflowForDuplicate(wf))}
                onDelete={() => {
                  setDeleteConfirmId(wf.id);
                  closeEditor();
                }}
              />
            )
          )}

          {query && filtered.length === 0 && (
            <p className="text-sm text-muted">No workflow matches &ldquo;{query}&rdquo;.</p>
          )}

          {defaults && !query && <DefaultCard defaults={defaults} />}
        </div>
      )}

      {defaults && (
        <p className="flex items-start gap-1.5 text-xs text-muted">
          <InformationCircleIcon className="mt-px h-4 w-4 shrink-0" aria-hidden="true" />
          <span>
            Workflows are files in <span className="font-mono">{defaults.storage_dir}/</span>, one
            folder each, next to the global prompts. Changes made there by hand are picked up
            automatically.
          </span>
        </p>
      )}

      {toast && <Toast toast={toast} onDismiss={dismissToast} />}
    </div>
  );
};

export default Workflows;
