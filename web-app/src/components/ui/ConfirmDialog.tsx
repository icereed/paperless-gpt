import React from "react";
import Button from "./Button";
import Modal from "./Modal";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
}

const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  open,
  title,
  body,
  confirmLabel,
  onConfirm,
  onCancel,
}) => (
  <Modal open={open} onClose={onCancel} title={title}>
    <div className="px-6 py-4">
      <p className="text-sm text-muted">{body}</p>
    </div>
    <div className="flex justify-end gap-2 border-t border-line px-6 py-4">
      <Button variant="secondary" onClick={onCancel}>
        Cancel
      </Button>
      <Button variant="danger" onClick={onConfirm}>
        {confirmLabel}
      </Button>
    </div>
  </Modal>
);

export default ConfirmDialog;
