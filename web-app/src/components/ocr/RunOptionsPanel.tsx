import { ChevronDownIcon, SparklesIcon } from "@heroicons/react/24/outline";
import classNames from "classnames";
import React, { useEffect, useState } from "react";
import Button from "../ui/Button";
import ConfirmDialog from "../ui/ConfirmDialog";
import {
  OCRConfig,
  OCRRunOptions,
  fetchOCRPromptTemplate,
  saveOCRDefaults,
  saveOCRPromptTemplate,
} from "./api";

interface RunOptionsPanelProps {
  config: OCRConfig;
  options: OCRRunOptions;
  onChange: (options: OCRRunOptions) => void;
  promptOverride: string | null;
  onPromptOverrideChange: (value: string | null) => void;
  onStart: () => void;
  canStart: boolean;
  running: boolean;
  onToast: (message: string) => void;
}

type PDFChoice = "none" | "attach" | "replace";

const pdfChoiceOf = (options: OCRRunOptions): PDFChoice =>
  !options.upload_pdf ? "none" : options.replace_original ? "replace" : "attach";

/**
 * Run Options: what this OCR Run does — page limit, process mode, searchable
 * PDF handling, and a run-scoped prompt override. Options that prove
 * themselves can be promoted to the auto-mode defaults from here.
 */
const RunOptionsPanel: React.FC<RunOptionsPanelProps> = ({
  config,
  options,
  onChange,
  promptOverride,
  onPromptOverrideChange,
  onStart,
  canStart,
  running,
  onToast,
}) => {
  const [promptOpen, setPromptOpen] = useState(promptOverride !== null);
  const [templateLoaded, setTemplateLoaded] = useState(false);
  const [confirmReplace, setConfirmReplace] = useState(false);
  const [savingDefaults, setSavingDefaults] = useState(false);
  const [savingPrompt, setSavingPrompt] = useState(false);

  // Opening the prompt editor for the first time seeds it with the saved template.
  useEffect(() => {
    if (!promptOpen || templateLoaded || promptOverride !== null) return;
    let cancelled = false;
    fetchOCRPromptTemplate()
      .then((template) => {
        if (!cancelled) {
          onPromptOverrideChange(template);
          setTemplateLoaded(true);
        }
      })
      .catch((err) => console.error("Failed to load OCR prompt template:", err));
    return () => {
      cancelled = true;
    };
  }, [promptOpen, templateLoaded, promptOverride, onPromptOverrideChange]);

  const pdfChoice = pdfChoiceOf(options);

  const setPdfChoice = (choice: PDFChoice) => {
    if (choice === "replace") {
      setConfirmReplace(true);
      return;
    }
    onChange({
      ...options,
      upload_pdf: choice === "attach",
      replace_original: false,
    });
  };

  const handleSaveDefaults = async () => {
    setSavingDefaults(true);
    try {
      await saveOCRDefaults(options);
      onToast(
        "Saved as defaults — Auto-OCR and future runs now use these options."
      );
    } catch (err) {
      console.error("Failed to save OCR defaults:", err);
      onToast("Saving defaults failed — check the backend connection.");
    } finally {
      setSavingDefaults(false);
    }
  };

  const handleSavePromptAsDefault = async () => {
    if (promptOverride === null) return;
    setSavingPrompt(true);
    try {
      await saveOCRPromptTemplate(promptOverride);
      onToast("Prompt saved as the global OCR template.");
    } catch (err) {
      console.error("Failed to save OCR prompt:", err);
      onToast("Saving the prompt failed — check the backend connection.");
    } finally {
      setSavingPrompt(false);
    }
  };

  const pdfChoices: { value: PDFChoice; label: string; description: string; disabled?: boolean }[] = [
    {
      value: "none",
      label: "No PDF",
      description: "Only the recognized text is produced.",
    },
    {
      value: "attach",
      label: "Attach as new document",
      description: "Uploads a searchable PDF next to the original.",
      disabled: !config.hocr_capable,
    },
    {
      value: "replace",
      label: "Replace original",
      description: "Deletes the original — permanent, not undoable.",
      disabled: !config.hocr_capable,
    },
  ];

  return (
    <div className="rounded-lg border border-line bg-surface p-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-wrap items-end gap-4">
          <label className="block">
            <span className="text-xs font-medium text-muted">Page limit</span>
            <input
              type="number"
              min={0}
              value={options.limit_pages}
              onChange={(e) =>
                onChange({
                  ...options,
                  limit_pages: Math.max(0, Number(e.target.value) || 0),
                })
              }
              disabled={running}
              aria-label="Page limit (0 processes all pages)"
              title="0 = all pages"
              className="mt-1 block h-9 w-24 rounded-md border border-line bg-surface px-2.5 text-sm"
            />
            <span className="mt-1 block text-xs text-faint">0 = all pages</span>
          </label>

          <label className="block">
            <span className="text-xs font-medium text-muted">Process mode</span>
            <select
              value={options.process_mode}
              onChange={(e) =>
                onChange({
                  ...options,
                  process_mode: e.target.value as OCRRunOptions["process_mode"],
                })
              }
              disabled={running}
              aria-label="Process mode"
              className="mt-1 block h-9 rounded-md border border-line bg-surface px-2.5 pr-8 text-sm"
            >
              <option value="image">Image per page</option>
              <option value="pdf">PDF per page</option>
              <option value="whole_pdf">Whole PDF at once</option>
            </select>
            <span className="mt-1 block text-xs text-faint">
              How pages reach the model
            </span>
          </label>

          <fieldset disabled={running}>
            <legend className="text-xs font-medium text-muted">
              Searchable PDF
            </legend>
            <div className="mt-1 flex flex-wrap gap-2" role="radiogroup">
              {pdfChoices.map((choice) => (
                <label
                  key={choice.value}
                  title={
                    choice.disabled
                      ? "Needs an hOCR-capable OCR provider"
                      : choice.description
                  }
                  className={classNames(
                    "inline-flex h-9 cursor-pointer select-none items-center rounded-md border px-3 text-sm transition-colors duration-150 ease-out-quart",
                    "focus-within:outline focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-primary",
                    choice.disabled && "cursor-not-allowed opacity-50",
                    pdfChoice === choice.value
                      ? choice.value === "replace"
                        ? "border-neg bg-neg-tint text-neg"
                        : "border-primary bg-primary-tint text-ink"
                      : "border-line text-muted hover:border-faint hover:text-ink"
                  )}
                >
                  <input
                    type="radio"
                    name="pdf-choice"
                    className="sr-only"
                    checked={pdfChoice === choice.value}
                    disabled={choice.disabled}
                    onChange={() => setPdfChoice(choice.value)}
                  />
                  {choice.label}
                </label>
              ))}
            </div>
            <span className="mt-1 block text-xs text-faint">
              {pdfChoice === "replace"
                ? "The original document will be deleted after upload."
                : options.limit_pages > 0
                  ? "PDFs need all pages — a page limit skips PDF creation on longer documents."
                  : "Adds an invisible text layer over the scan."}
            </span>
          </fieldset>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleSaveDefaults}
            loading={savingDefaults}
            disabled={running}
            title="Auto-OCR and future runs use these options"
          >
            Save as defaults
          </Button>
          <Button variant="primary" onClick={onStart} disabled={!canStart} loading={running}>
            <SparklesIcon className="h-4 w-4" aria-hidden="true" />
            Run OCR
          </Button>
        </div>
      </div>

      <div className="mt-4 border-t border-line pt-3">
        <button
          type="button"
          onClick={() => setPromptOpen((v) => !v)}
          aria-expanded={promptOpen}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-muted hover:text-ink"
        >
          <ChevronDownIcon
            className={classNames(
              "h-4 w-4 transition-transform duration-150 ease-out-quart",
              promptOpen && "rotate-180"
            )}
            aria-hidden="true"
          />
          Prompt
          {promptOverride !== null && templateLoaded && (
            <span className="rounded-full bg-primary-tint px-2 py-0.5 text-xs font-medium">
              customized for this run
            </span>
          )}
        </button>

        {promptOpen && (
          <div className="mt-2">
            <textarea
              value={promptOverride ?? ""}
              onChange={(e) => onPromptOverrideChange(e.target.value)}
              disabled={running}
              rows={8}
              aria-label="OCR prompt for this run"
              spellCheck={false}
              className="w-full rounded-md border border-line bg-surface px-3 py-2 font-mono text-xs leading-relaxed text-ink"
            />
            <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
              <p className="text-xs text-faint">
                Applies to this run only — the saved template stays untouched.
                Placeholders: {"{{ .Language }}"}, {"{{ .Content }}"}.
              </p>
              <div className="flex gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={running}
                  onClick={() => {
                    onPromptOverrideChange(null);
                    setTemplateLoaded(false);
                    setPromptOpen(false);
                  }}
                >
                  Discard changes
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleSavePromptAsDefault}
                  loading={savingPrompt}
                  disabled={running || promptOverride === null}
                  title="Writes the global OCR prompt template"
                >
                  Save as default prompt
                </Button>
              </div>
            </div>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmReplace}
        title="Replace originals permanently?"
        body="After OCR, the searchable PDF is uploaded and the original document is deleted from paperless-ngx. This cannot be undone — the History page cannot restore deleted documents."
        confirmLabel="Replace original"
        onConfirm={() => {
          onChange({ ...options, upload_pdf: true, replace_original: true });
          setConfirmReplace(false);
        }}
        onCancel={() => setConfirmReplace(false)}
      />
    </div>
  );
};

export default RunOptionsPanel;
