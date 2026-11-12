import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";

/** A titled block of text with a copy button, for files and commands an admin pastes elsewhere. */
export function CopyBlock({ title, hint, text }: { title: string; hint?: string; text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-white">{title}</div>
          {hint && <div className="text-xs text-zinc-400">{hint}</div>}
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            void navigator.clipboard.writeText(text);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          }}
        >
          {copied ? <Check className="size-3.5 text-emerald-400" /> : <Copy className="size-3.5" strokeWidth={1.5} />}
          <span>{copied ? "Copied" : "Copy"}</span>
        </Button>
      </div>
      <pre className="max-h-72 overflow-auto rounded-lg bg-[#060608] p-3 text-xs font-mono leading-relaxed text-zinc-300 whitespace-pre-wrap break-all">{text}</pre>
    </div>
  );
}
