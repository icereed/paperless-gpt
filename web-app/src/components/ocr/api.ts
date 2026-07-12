import axios from "axios";
import { Document } from "../../DocumentProcessor";

export interface OCRRunOptions {
  limit_pages: number;
  process_mode: "image" | "pdf" | "whole_pdf";
  upload_pdf: boolean;
  replace_original: boolean;
  copy_metadata: boolean;
}

export interface OCRConfig {
  enabled: boolean;
  provider: string;
  hocr_capable: boolean;
  defaults: OCRRunOptions;
  auto_tag: string;
  ocr_complete_tag: string;
  ocr_tagging: boolean;
  auto_queue_count?: number;
}

export interface OCRRun {
  id: number;
  job_id: string;
  document_id: number;
  document_title: string;
  trigger: "manual" | "auto";
  status: "in_progress" | "completed" | "failed" | "cancelled" | "interrupted";
  limit_pages: number;
  process_mode: string;
  upload_pdf: boolean;
  replace_original: boolean;
  copy_metadata: boolean;
  prompt_overridden: boolean;
  prompt_override?: string;
  provider: string;
  pages_done: number;
  total_pages: number;
  pdf_action: "none" | "attached" | "replaced" | "skipped" | "failed" | "";
  pdf_detail?: string;
  error?: string;
  started_at: string;
  finished_at?: string;
}

export interface OCRJobStatus {
  job_id: string;
  status: "pending" | "in_progress" | "completed" | "failed" | "cancelled";
  pages_done: number;
  total_pages: number;
  result?: string;
  error?: string;
}

export interface OCRPage {
  pageIndex: number;
  text: string;
  ocrLimitHit: boolean;
  generationInfo?: Record<string, unknown>;
}

export async function fetchOCRConfig(): Promise<OCRConfig> {
  const { data } = await axios.get<OCRConfig>("./api/ocr/config");
  return data;
}

export async function searchDocuments(query: string): Promise<Document[]> {
  const { data } = await axios.get<{ documents: Document[] }>(
    "./api/search-documents",
    { params: { q: query } }
  );
  return data.documents || [];
}

export async function submitOCRJob(
  documentId: number,
  options: OCRRunOptions,
  promptOverride: string | null
): Promise<string> {
  const { data } = await axios.post<{ job_id: string }>(
    `./api/documents/${documentId}/ocr`,
    {
      limit_pages: options.limit_pages,
      process_mode: options.process_mode,
      upload_pdf: options.upload_pdf,
      replace_original: options.replace_original,
      copy_metadata: options.copy_metadata,
      ...(promptOverride ? { prompt_override: promptOverride } : {}),
    }
  );
  return data.job_id;
}

export async function fetchOCRJob(jobId: string): Promise<OCRJobStatus> {
  const { data } = await axios.get<OCRJobStatus>(`./api/jobs/ocr/${jobId}`);
  return data;
}

export async function stopOCRJob(jobId: string): Promise<void> {
  await axios.post(`./api/ocr/jobs/${jobId}/stop`);
}

export async function fetchOCRRuns(
  documentId?: number,
  limit = 50
): Promise<{ runs: OCRRun[]; total: number }> {
  const { data } = await axios.get<{ runs: OCRRun[]; total: number }>(
    "./api/ocr/runs",
    { params: { document_id: documentId || undefined, limit } }
  );
  return { runs: data.runs || [], total: data.total };
}

export async function fetchOCRPages(
  documentId: number,
  jobId: string
): Promise<{ pages: OCRPage[]; job_id: string }> {
  const { data } = await axios.get<{ pages: OCRPage[]; job_id: string }>(
    `./api/documents/${documentId}/ocr_pages`,
    { params: { job_id: jobId || undefined } }
  );
  return { pages: data.pages || [], job_id: data.job_id };
}

export async function reOCRPage(
  documentId: number,
  pageIndex: number,
  jobId: string,
  signal?: AbortSignal
): Promise<OCRPage> {
  const { data } = await axios.post<{
    text: string;
    ocrLimitHit: boolean;
    generationInfo?: Record<string, unknown>;
  }>(
    `./api/documents/${documentId}/ocr_pages/${pageIndex}/reocr`,
    null,
    { params: { job_id: jobId || undefined }, signal }
  );
  return { pageIndex, text: data.text, ocrLimitHit: data.ocrLimitHit, generationInfo: data.generationInfo };
}

export async function applyContent(
  document: Document,
  content: string
): Promise<void> {
  await axios.patch("./api/update-documents", [
    {
      id: document.id,
      original_document: document,
      suggested_content: content,
    },
  ]);
}

export async function saveOCRDefaults(options: OCRRunOptions): Promise<void> {
  await axios.put("./api/ocr/defaults", {
    limit_pages: options.limit_pages,
    process_mode: options.process_mode,
    upload_pdf: options.upload_pdf,
    replace_original: options.replace_original,
    copy_metadata: options.copy_metadata,
  });
}

export async function fetchOCRPromptTemplate(): Promise<string> {
  const { data } = await axios.get<Record<string, string>>("./api/prompts");
  return data["ocr_prompt.tmpl"] || "";
}

export async function saveOCRPromptTemplate(content: string): Promise<void> {
  await axios.post("./api/prompts", {
    filename: "ocr_prompt.tmpl",
    content,
  });
}

export function formatRunOptions(run: OCRRun): string {
  const parts: string[] = [];
  parts.push(run.process_mode || "image");
  parts.push(run.limit_pages > 0 ? `max ${run.limit_pages} pages` : "all pages");
  if (run.upload_pdf) {
    parts.push(run.replace_original ? "PDF replaces original" : "PDF attached");
  }
  if (run.prompt_overridden) {
    parts.push("custom prompt");
  }
  return parts.join(" · ");
}

export function runDuration(run: OCRRun): string | null {
  if (!run.finished_at) return null;
  const ms = new Date(run.finished_at).getTime() - new Date(run.started_at).getTime();
  if (ms < 0) return null;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}
