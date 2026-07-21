import {
  CheckCircleIcon,
  InformationCircleIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import React, { useEffect } from "react";
import { Link } from "react-router-dom";

export interface ToastData {
  kind: "success" | "info";
  message: string;
  action?: { label: string; to: string };
}

interface ToastProps {
  toast: ToastData;
  onDismiss: () => void;
}

const AUTO_DISMISS_MS = 8000;

const Toast: React.FC<ToastProps> = ({ toast, onDismiss }) => {
  useEffect(() => {
    const timer = setTimeout(onDismiss, AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  }, [toast, onDismiss]);

  const Icon = toast.kind === "success" ? CheckCircleIcon : InformationCircleIcon;

  return (
    <div
      role="status"
      className="fixed bottom-6 right-6 z-toast flex max-w-md items-start gap-3 rounded-lg border border-line bg-surface p-4 shadow-raised"
    >
      <Icon
        className={
          toast.kind === "success"
            ? "h-5 w-5 shrink-0 text-pos"
            : "h-5 w-5 shrink-0 text-muted"
        }
        aria-hidden="true"
      />
      <div className="min-w-0 text-sm">
        <p>{toast.message}</p>
        {toast.action && (
          <Link
            to={toast.action.to}
            className="mt-1 inline-block font-medium text-primary hover:underline"
            onClick={onDismiss}
          >
            {toast.action.label} →
          </Link>
        )}
      </div>
      <button
        type="button"
        onClick={onDismiss}
        aria-label="Dismiss notification"
        className="rounded p-0.5 text-faint hover:text-ink"
      >
        <XMarkIcon className="h-4 w-4" aria-hidden="true" />
      </button>
    </div>
  );
};

export default Toast;
