import * as React from "react";
import { Link } from "react-router-dom";
import { Check, ChevronDown, Layers } from "lucide-react";
import type { App } from "@/lib/api";
import { worstStatus } from "@/lib/appstatus";
import { StatusBadge } from "@/components/StatusBadge";

/**
 * Jump to another app of the same repository without going back to the list. The environment and the tab stay the
 * same where the other app has them, so moving from one service's logs to a sibling's logs is one click.
 * Nothing is shown when the repository has only this app.
 */
export function AppSwitcher({ current, apps, env, tab }: { current: App; apps: App[]; env: string; tab?: string }) {
  const [open, setOpen] = React.useState(false);
  const box = React.useRef<HTMLDivElement>(null);
  React.useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [open]);

  const siblings = apps.filter((a) => a.repo === current.repo).sort((a, b) => a.name.localeCompare(b.name));
  if (siblings.length < 2) return null;

  const target = (a: App) => {
    const sameEnv = a.environments.some((e) => e.env === env);
    return sameEnv ? `/apps/${a.id}/${env}${tab ? `/${tab}` : ""}` : `/apps/${a.id}`;
  };

  return (
    <div ref={box} className="relative">
      <button
        type="button"
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="flex h-8 items-center gap-2 rounded-md bg-[#121215] px-2.5 text-xs font-mono text-zinc-200 hover:bg-[#141418] cursor-pointer"
      >
        <Layers className="size-3.5 text-zinc-500" strokeWidth={1.5} />
        Switch app
        <span className="rounded bg-[#202028] px-1.5 text-white">{siblings.length}</span>
        <ChevronDown className="size-3.5 text-zinc-500" strokeWidth={1.5} />
      </button>
      {open && (
        <nav aria-label="Apps in this repository" className="absolute left-0 z-20 mt-1.5 w-72 rounded-lg bg-[#141418] p-1.5 shadow-xl shadow-black/60">
          <p className="truncate px-2 py-1.5 text-xs font-mono text-zinc-500">{current.repo}</p>
          <ul>
            {siblings.map((a) => {
              const isCurrent = a.id === current.id;
              return (
                <li key={a.id}>
                  <Link
                    to={target(a)}
                    aria-current={isCurrent ? "page" : undefined}
                    onClick={() => setOpen(false)}
                    className="flex items-center gap-2.5 rounded-md px-2 py-2 text-sm hover:bg-[#202028] focus:outline-none focus-visible:bg-[#202028]"
                  >
                    <span className="flex size-4 items-center justify-center">{isCurrent && <Check className="size-3.5 text-white" strokeWidth={2} />}</span>
                    <span className={`flex-1 truncate ${isCurrent ? "font-semibold text-white" : "text-zinc-300"}`}>{a.name}</span>
                    <StatusBadge status={worstStatus(a.environments)} size="sm" showLabel={false} />
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>
      )}
    </div>
  );
}
