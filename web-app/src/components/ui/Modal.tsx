import { Dialog, DialogPanel, DialogTitle } from "@headlessui/react";
import classNames from "classnames";
import React from "react";

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: React.ReactNode;
  /** Visually hide the title (it stays available to screen readers). */
  hideTitle?: boolean;
  size?: "md" | "lg" | "focus";
  initialFocus?: React.RefObject<HTMLElement | null>;
  children: React.ReactNode;
}

const sizeClasses = {
  md: "w-full max-w-md",
  lg: "w-full max-w-2xl",
  focus: "w-full max-w-6xl h-[85vh]",
};

const Modal: React.FC<ModalProps> = ({
  open,
  onClose,
  title,
  hideTitle,
  size = "md",
  initialFocus,
  children,
}) => (
  <Dialog
    open={open}
    onClose={onClose}
    initialFocus={initialFocus}
    className="relative z-modal"
  >
    <div
      className="fixed inset-0 z-modal-backdrop bg-black/50"
      aria-hidden="true"
    />
    <div className="fixed inset-0 z-modal flex items-center justify-center p-4">
      <DialogPanel
        transition
        className={classNames(
          "flex max-h-full flex-col overflow-hidden rounded-lg border border-line bg-surface shadow-raised",
          "duration-200 ease-out-quart data-[closed]:scale-[0.98] data-[closed]:opacity-0",
          sizeClasses[size]
        )}
      >
        <DialogTitle
          className={classNames(
            "shrink-0 text-lg font-semibold",
            hideTitle ? "sr-only" : "border-b border-line px-6 py-4"
          )}
        >
          {title}
        </DialogTitle>
        {children}
      </DialogPanel>
    </div>
  </Dialog>
);

export default Modal;
