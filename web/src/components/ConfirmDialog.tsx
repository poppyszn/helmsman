import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

type Props = {
  open: boolean;
  title: string;
  body: React.ReactNode;
  confirmLabel: string;
  requireText?: string;
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
};

export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  requireText,
  busy,
  error,
  onConfirm,
  onCancel,
}: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const [typed, setTyped] = useState("");

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      setTyped("");
      d.showModal();
    }
    if (!open && d.open) d.close();
  }, [open]);

  const ok = !requireText || typed === requireText;

  return (
    <dialog
      ref={ref}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onCancel();
      }}
      className="m-auto w-full max-w-md rounded-xl bg-[#0c0c10] p-6 text-zinc-100 shadow-2xl backdrop:bg-black/80 backdrop:backdrop-blur-sm"
    >
      <div>
        <h2 className="text-base font-semibold text-white tracking-tight">{title}</h2>
        <div className="mt-2 text-sm text-zinc-300 leading-relaxed">{body}</div>
      </div>

      {requireText && (
        <div className="mt-4 rounded-lg bg-[#141418] p-3 text-xs">
          <label className="block text-zinc-400 text-xs font-mono">
            Type <code className="text-white font-semibold">{requireText}</code> to confirm:
            <Input
              className="mt-2 font-mono text-xs h-9 bg-black"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              placeholder={requireText}
            />
          </label>
        </div>
      )}

      {error && (
        <p role="alert" className="mt-3 rounded-lg bg-rose-500/10 p-2.5 text-xs text-rose-300 font-mono">
          {error}
        </p>
      )}

      <div className="mt-5 flex justify-end gap-2.5 pt-3 border-t border-zinc-900">
        <Button variant="ghost" size="default" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button
          variant="destructive"
          size="default"
          onClick={onConfirm}
          disabled={!ok || busy}
        >
          {busy ? "Working..." : confirmLabel}
        </Button>
      </div>
    </dialog>
  );
}
