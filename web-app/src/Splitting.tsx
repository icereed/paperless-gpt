import { useEffect, useState } from 'react';
import Button from './components/ui/Button';
import ConfirmDialog from './components/ui/ConfirmDialog';
import Modal from './components/ui/Modal';

interface Boundary {
  after_page: number;
  confidence: number;
  reason: string;
  conflict?: boolean;
  blocked_reason?: string;
}

interface Suggestion {
  id: number;
  document_id: number;
  document_title: string;
  page_count: number;
  status: string;
  boundaries: Boundary[] | null;
  applied_boundaries?: number[];
  updated_at: string;
  error?: string;
}

interface SplitList {
  results: Suggestion[];
  count: number;
  config: { dry_run: boolean; delete_originals: boolean; tag: string };
}

const statusLabels: Record<string, string> = {
  review: 'Awaiting Review', skipped: 'Skipped', submitted: 'Submitted', kept: 'Kept as one document',
  uncertain: 'Needs inspection', failed: 'Failed', generating: 'Generating', applying: 'Applying',
};

/** Returns JSON from a split API request, throwing the API error or status on HTTP failure. */
async function splitRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`./api/${path}`, init);
  if (!response.ok) {
    const data = await response.json().catch(() => null);
    throw new Error(data?.error || `Request failed (${response.status})`);
  }
  return response.json();
}

/** Opens an available page preview or shows a fallback when its image cannot load. */
function PageThumbnail({ src, page, onOpen }: { src: string; page: number; onOpen: () => void }) {
  const [failed, setFailed] = useState(false);
  return failed ? (
    <div className="flex h-52 items-center justify-center rounded border border-line bg-surface-2 text-sm text-muted">Preview unavailable for page {page}</div>
  ) : (
    <button type="button" onClick={onOpen} aria-label={`Enlarge page ${page}`} className="block w-full rounded border border-line bg-surface-2 p-2">
      <img src={src} alt={`Page ${page}`} loading="lazy" onError={() => setFailed(true)} className="h-52 w-full object-contain" />
    </button>
  );
}

/** Edits one suggestion's boundaries and requires a refresh after an action's response is lost. */
function SplitReview({ suggestion, config, busy, setBusy, onUpdate, onSkip }: {
  suggestion: Suggestion;
  config: SplitList['config'];
  busy: boolean;
  setBusy: (value: boolean) => void;
  onUpdate: (value: Suggestion, message?: string) => void;
  onSkip: (value: Suggestion) => void;
}) {
  const [boundaries, setBoundaries] = useState<number[]>(() => suggestion.status === 'kept' ? [] : suggestion.applied_boundaries ?? (suggestion.boundaries ?? []).map(b => b.after_page));
  const [error, setError] = useState('');
  const [needsRefresh, setNeedsRefresh] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [enlarged, setEnlarged] = useState<number | null>(null);
  const editable = ['review', 'skipped'].includes(suggestion.status) && !busy && !needsRefresh;
  const ranges = [...boundaries, suggestion.page_count].map((last, index) => {
    const first = index === 0 ? 1 : boundaries[index - 1] + 1;
    return first === last ? `${first}` : `${first}-${last}`;
  });
  /** Converts a one-based page number to an image URL keyed by the suggestion's revision. */
  const imageURL = (page: number) => `./api/documents/${suggestion.document_id}/pages/${page - 1}/image?revision=${encodeURIComponent(suggestion.updated_at)}`;
  const hasRuleOverride = (suggestion.boundaries ?? []).some(b => boundaries.includes(b.after_page) && (b.blocked_reason || b.conflict));

  /** Adds or removes the boundary after a one-based page while keeping boundaries sorted. */
  const toggle = (page: number) => {
    setBoundaries(current => current.includes(page) ? current.filter(p => p !== page) : [...current, page].sort((a, b) => a - b));
  };

  /** Submits a review action and blocks repeat submissions after any failed response. */
  const act = async (action: 'apply' | 'skip' | 'generate') => {
    setConfirm(false);
    setBusy(true);
    setError('');
    try {
      const path = action === 'generate' ? `documents/${suggestion.document_id}/split-suggestions` : `split-suggestions/${suggestion.id}/${action}`;
      const result = await splitRequest<Suggestion>(path, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        ...(action === 'apply' ? { body: JSON.stringify({ boundaries }) } : {}),
      });
      if (action === 'skip') onSkip({ ...suggestion, status: 'skipped' });
      else onUpdate(result, action === 'apply' ? (result.status === 'kept' ? 'Document kept intact and released for processing.' : 'Split submitted. Check paperless-ngx for the new documents.') : 'Suggestion generated.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed');
      // A lost response does not mean Apply failed. Refresh before allowing another submission.
      setNeedsRefresh(true);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section aria-labelledby="split-review-heading" className="min-w-0 rounded-lg border border-line bg-surface p-4 sm:p-6">
      <h2 id="split-review-heading" className="break-words text-xl font-semibold">Review: {suggestion.document_title || `Document ${suggestion.document_id}`}</h2>
      <p className="mt-1 text-sm text-muted">Document {suggestion.document_id} · {suggestion.page_count} pages · {statusLabels[suggestion.status] ?? suggestion.status}</p>
      {suggestion.status === 'submitted' && <p className="mt-3 text-sm text-muted">The split was submitted. New documents are consumed asynchronously by paperless-ngx.</p>}
      {suggestion.status === 'uncertain' && <p className="mt-3 text-sm text-neg">Check the documents and task log in paperless-ngx before splitting this batch again.</p>}
      {suggestion.error && <p className="mt-3 break-words text-sm text-neg">{suggestion.error}</p>}
      {error && <p role="alert" className="mt-3 break-words text-sm text-neg">{error} Refresh the list to check the current state.</p>}
      {suggestion.page_count > 0 && <p className="mt-4 font-medium" aria-live="polite">{ranges.length} {ranges.length === 1 ? 'document' : 'documents'}: {ranges.join(', ')}</p>}
      {editable && <p className="mt-2 text-sm text-muted">Toggle the markers after each page to change document boundaries. Skip discards your edits and keeps the Suggestion for later Review.</p>}
      {hasRuleOverride && <p className="mt-3 rounded-md bg-warn-tint p-3 text-sm text-warn">Some selected boundaries conflict with pagination or overlapping windows. Review the marked pages before Apply.</p>}
      <div className="mt-4 flex flex-wrap gap-2">
        <Button variant="primary" disabled={!editable || config.dry_run || boundaries.length === 0} onClick={() => setConfirm(true)}>Apply</Button>
        {boundaries.length === 0 && <Button disabled={!editable || config.dry_run} onClick={() => setConfirm(true)}>Keep as one document</Button>}
        <Button disabled={!editable} onClick={() => void act('skip')}>Skip</Button>
        {['review', 'skipped', 'failed'].includes(suggestion.status) && <Button disabled={busy || needsRefresh} onClick={() => void act('generate')}>Generate again</Button>}
        {busy && <span className="self-center text-sm text-muted">Processing...</span>}
      </div>
      {config.dry_run && <p className="mt-2 text-sm text-muted">Dry-run is enabled. Apply and Keep as one document are disabled.</p>}
      {!boundaries.length && editable && <p className="mt-2 text-sm text-muted">Keep this document intact to continue processing, or select a boundary to split it.</p>}
      <ol className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: suggestion.page_count }, (_, index) => {
          const page = index + 1;
          const boundary = suggestion.boundaries?.find(b => b.after_page === page);
          const selected = boundaries.includes(page);
          return (
            <li key={page} className="min-w-0">
              <p className="mb-2 text-sm font-medium">Page {page}</p>
              <PageThumbnail src={imageURL(page)} page={page} onOpen={() => setEnlarged(page)} />
              {page < suggestion.page_count && (
                <div className="mt-2 border-t border-dashed border-line pt-2">
                  <Button className="w-full" variant={selected ? 'primary' : 'secondary'} aria-label={`Split after page ${page}`} aria-pressed={selected} disabled={!editable} onClick={() => toggle(page)}>
                    {selected ? 'Split here' : 'Keep together'}
                  </Button>
                  {boundary && <p className="mt-2 break-words text-xs text-muted">Suggestion: {Math.round(boundary.confidence * 100)}% confidence. {boundary.reason}</p>}
                  {boundary?.conflict && <p className="mt-1 text-xs text-warn">Overlapping windows disagree.</p>}
                  {boundary?.blocked_reason && <p className="mt-1 break-words text-xs text-warn">{boundary.blocked_reason}</p>}
                </div>
              )}
            </li>
          );
        })}
      </ol>
      <ConfirmDialog open={confirm} title={boundaries.length ? 'Apply split' : 'Keep as one document'} confirmLabel={boundaries.length ? 'Apply split' : 'Keep document'} onCancel={() => setConfirm(false)} onConfirm={() => void act('apply')}
        body={boundaries.length ? `Create ${ranges.length} documents with pages ${ranges.join(', ')}. The original will be ${config.delete_originals ? 'deleted after the new documents are consumed' : 'kept'}.${hasRuleOverride ? ' Your selected boundaries override the marked pagination or window conflicts.' : ''}` : 'Keep every page in this document, remove the split tag and apply output tags to continue processing.'} />
      <Modal open={enlarged !== null} onClose={() => setEnlarged(null)} title={`Page ${enlarged ?? ''}`} size="lg">
        <div className="overflow-auto p-4">{enlarged !== null && <img src={imageURL(enlarged)} alt={`Enlarged page ${enlarged}`} className="w-full" />}</div>
        <div className="flex justify-end border-t border-line p-3"><Button onClick={() => setEnlarged(null)}>Close</Button></div>
      </Modal>
    </section>
  );
}

/** Lists paginated split suggestions and coordinates selection, review actions, and refreshes. */
export default function Splitting() {
  const [data, setData] = useState<SplitList | null>(null);
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    let ignore = false;
    splitRequest<SplitList>(`split-suggestions?page=${page}`, { signal: controller.signal }).then(result => {
      if (!ignore) { setData(result); setLoading(false); }
    }).catch(err => {
      if (!ignore) { setError(err instanceof Error ? err.message : 'Cannot load Suggestions'); setLoading(false); }
    });
    return () => { ignore = true; controller.abort(); };
  }, [page, revision]);

  /** Reloads the requested page, clearing selection and notices even when the page is unchanged. */
  const refresh = (nextPage = page) => {
    setLoading(true); setError(''); setMessage(''); setSelectedId(null);
    setPage(nextPage); setRevision(current => current + 1);
  };
  /** Replaces a visible suggestion after an action and displays its optional notice. */
  const update = (suggestion: Suggestion, notice = '') => {
    setData(current => current && ({ ...current, results: current.results.map(item => item.id === suggestion.id ? suggestion : item) }));
    setMessage(notice);
  };
  const selected = data?.results.find(s => s.id === selectedId);
  const totalPages = Math.max(1, Math.ceil((data?.count ?? 0) / 50));

  return (
    <div className="mx-auto max-w-7xl p-4 text-ink sm:p-8">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div><h1 className="text-2xl font-semibold">Split Suggestions</h1><p className="mt-1 text-sm text-muted">Review document boundaries in batch scans.</p></div>
        <Button disabled={busy || loading} onClick={() => refresh()}>Refresh</Button>
      </header>
      {message && <p role="status" className="mb-4 rounded-md bg-pos-tint p-3 text-sm text-pos">{message}</p>}
      {error && <p role="alert" className="mb-4 text-sm text-neg">{error}</p>}
      {loading ? <p className="text-muted">Loading Suggestions...</p> : data && (
        <div className="grid items-start gap-6 lg:grid-cols-[16rem_minmax(0,1fr)]">
          <section aria-label="Split Suggestion list">
            <p className="mb-3 text-sm text-muted">{data.count} {data.count === 1 ? 'Suggestion' : 'Suggestions'}</p>
            {data.results.length === 0 && <p className="text-sm text-muted">No Suggestions yet. Add the tag <strong>{data.config.tag}</strong> to a batch PDF in paperless-ngx, then refresh.</p>}
            <ul className="space-y-2">{data.results.map(s => (
              <li key={s.id}>
                <button type="button" disabled={busy} aria-label={`Review ${s.document_title || `document ${s.document_id}`}`} aria-pressed={s.id === selectedId}
                  className={`w-full rounded-md border p-3 text-left disabled:opacity-50 ${s.id === selectedId ? 'border-primary bg-primary-tint' : 'border-line bg-surface hover:bg-surface-2'}`}
                  onClick={() => { setSelectedId(s.id); setMessage(''); }}>
                  <span className="block break-words text-sm font-medium">{s.document_title || `Document ${s.document_id}`}</span>
                  <span className="mt-1 block text-xs text-muted">#{s.document_id} · {s.page_count} pages · {statusLabels[s.status] ?? s.status}</span>
                </button>
              </li>
            ))}</ul>
            <div className="mt-4 flex flex-wrap items-center gap-2">
              <Button size="sm" disabled={busy || page <= 1} onClick={() => refresh(page - 1)}>Previous</Button>
              <span className="text-xs text-muted">{page} / {totalPages}</span>
              <Button size="sm" disabled={busy || page >= totalPages} onClick={() => refresh(page + 1)}>Next</Button>
            </div>
          </section>
          {selected ? <SplitReview key={`${selected.id}:${selected.updated_at}`} suggestion={selected} config={data.config} busy={busy} setBusy={setBusy}
            onUpdate={update} onSkip={suggestion => { update(suggestion, 'Skipped. The Suggestion remains available for Review.'); setSelectedId(null); }} /> : <p className="rounded-lg border border-line bg-surface p-6 text-sm text-muted">Select a Suggestion to Review its pages.</p>}
        </div>
      )}
    </div>
  );
}
