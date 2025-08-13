export type OCRJobStatus = 'idle' | 'pending' | 'in_progress' | 'completed' | 'failed' | 'cancelled';
export type ClientStatus = 'idle' | 'fetching_details' | 'submitting';

export type StatusViewOptions = {
  label: string;
  showSpinner: boolean;
  canStop: boolean;
};

export const mapJobStatus = (raw: string | null | undefined): OCRJobStatus => {
  switch (raw) {
    case 'pending':
    case 'in_progress':
    case 'completed':
    case 'failed':
    case 'cancelled':
      return raw;
    default:
      throw new Error(`Unknown job status: ${raw}`);
  }
};

export const getStatusViewOptions = (job: OCRJobStatus, clientStatus: ClientStatus): StatusViewOptions => {
  if (clientStatus === 'fetching_details') {
    return { label: 'Fetching document details...', showSpinner: true, canStop: false };
  }
  if (clientStatus === 'submitting') {
    return { label: 'Submitting OCR job...', showSpinner: true, canStop: false };
  }
  switch (job) {
    case 'idle':
      return { label: '', showSpinner: false, canStop: false };
    case 'pending':
      return { label: 'Job status: pending. This may take a few minutes.', showSpinner: true, canStop: true };
    case 'in_progress':
      return { label: 'Job status: in progress. This may take a few minutes.', showSpinner: true, canStop: true };
    case 'completed':
      return { label: 'OCR completed successfully.', showSpinner: false, canStop: false };
    case 'failed':
      return { label: 'OCR failed.', showSpinner: false, canStop: false };
    case 'cancelled':
      return { label: 'Job cancelled by user.', showSpinner: false, canStop: false };
  }
};
