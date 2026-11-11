import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";

type Props = {
  open: boolean;
  title: string;
  body: React.ReactNode;
  confirmLabel: string;
  // When set, the user must type this exact text to enable the confirm button (production).
  requireText?: string;
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
};

export function ConfirmDialog({ open, title, body, confirmLabel, requireText, busy, error, onConfirm, onCancel }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const [typed, setTyped] = useState("");
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) { setTyped(""); d.showModal(); }
    if (!open && d.open) d.close();
  }, [open]);
  const ok = !requireText || typed === requireText;
  return (
    <dialog ref={ref} onCancel={(e) => { e.preventDefault(); if (!busy) onCancel(); }}
      className="m-auto w-full max-w-md rounded-lg border border-border bg-card p-5 text-foreground backdrop:bg-black/50">
      <h2 className="text-lg font-semibold">{title}</h2>
      <div className="mt-2 text-sm">{body}</div>
      {requireText && (
        <label className="mt-3 block text-sm">
          Type <code className="rounded bg-border/60 px-1">{requireText}</code> to confirm
          <input className="mt-1 h-9 w-full rounded-md border border-border bg-background px-2 text-sm" value={typed}
            onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
        </label>
      )}
      {error && <p role="alert" className="mt-3 text-sm text-red-600 dark:text-red-400">{error}</p>}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="outline" onClick={onCancel} disabled={busy}>Cancel</Button>
        <Button onClick={onConfirm} disabled={!ok || busy}>{busy ? "Working" : confirmLabel}</Button>
      </div>
    </dialog>
  );
}
