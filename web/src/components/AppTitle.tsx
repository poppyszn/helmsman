import * as React from "react";
import { Link } from "react-router-dom";
import { Check, ChevronsUpDown } from "lucide-react";
import type { App } from "@/lib/api";
import { worstStatus } from "@/lib/appstatus";
import { menuKeyDown, useDismiss, useFocusItem } from "@/lib/useDismiss";
import { StatusBadge } from "@/components/StatusBadge";

/**
 * The app's title. When the repository has other apps the title is also the switcher, like a project picker: click it
 * to jump to a sibling without going back to the list. The environment and the tab stay the same where the other app
 * has them, so moving from one service's logs to a sibling's logs is one click.
 */
export function AppTitle({ current, apps, env, tab }: { current: App; apps: App[]; env: string; tab?: string }) {
  const [open, setOpen] = React.useState(false);
  const box = React.useRef<HTMLDivElement>(null);
  const close = React.useCallback(() => setOpen(false), []);
  useDismiss(open, box, close);
  useFocusItem(open, box);

  const siblings = apps.filter((a) => a.repo === current.repo).sort((a, b) => a.name.localeCompare(b.name));
  const title = "text-2xl font-bold text-white tracking-tight";
  if (siblings.length < 2) return <h1 className={title}>{current.name}</h1>;

  const target = (a: App) => {
    const sameEnv = a.environments.some((e) => e.env === env);
    return sameEnv ? `/apps/${a.id}/${env}${tab ? `/${tab}` : ""}` : `/apps/${a.id}`;
  };

  return (
    <div ref={box} className="relative">
      <h1 className={title}>
        <button
          type="button"
          title="Switch to another app in this repository"
          aria-haspopup="true"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
          className={`-ml-2.5 flex items-center gap-2 rounded-lg px-2.5 py-1 text-left tracking-tight transition-colors cursor-pointer hover:bg-[#121215] focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-zinc-400 ${open ? "bg-[#121215]" : ""}`}
        >
          {current.name}
          <ChevronsUpDown className="size-4 text-zinc-500" strokeWidth={1.5} />
        </button>
      </h1>
      {open && (
        <nav aria-label="Apps in this repository" onKeyDown={menuKeyDown} className="absolute left-0 z-20 mt-2 w-80 origin-top-left animate-pop-in rounded-xl bg-[#0c0c10] p-2 shadow-2xl">
          <p className="px-2.5 pb-1.5 pt-1 text-xs font-mono text-zinc-500">
            {siblings.length} apps in <span className="text-zinc-300">{current.repo}</span>
          </p>
          <ul className="space-y-0.5">
            {siblings.map((a) => {
              const isCurrent = a.id === current.id;
              return (
                <li key={a.id}>
                  <Link
                    to={target(a)}
                    data-menu-item
                    aria-current={isCurrent ? "page" : undefined}
                    onClick={() => setOpen(false)}
                    className={`flex items-center gap-3 rounded-lg px-2.5 py-2 text-sm focus:outline-none focus-visible:bg-[#141418] ${isCurrent ? "bg-[#1c1c22]" : "hover:bg-[#141418]"}`}
                  >
                    <StatusBadge status={worstStatus(a.environments)} size="sm" showLabel={false} />
                    <span className={`flex-1 truncate ${isCurrent ? "font-semibold text-white" : "text-zinc-300"}`}>{a.name}</span>
                    {isCurrent ? (
                      <Check className="size-3.5 text-white" strokeWidth={2} />
                    ) : (
                      <span className="font-mono text-xs text-zinc-500">{a.environments.map((e) => e.env).join(", ")}</span>
                    )}
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
