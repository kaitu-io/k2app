interface PasswordDialogProps {
  open?: boolean;
  onClose?: () => void;
}

export function PasswordDialog({ open, onClose }: PasswordDialogProps) {
  if (!open) return null;
  return (
    <div data-testid="password-dialog">
      <button onClick={onClose}>Close</button>
    </div>
  );
}
