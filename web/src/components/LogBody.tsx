import { AnsiText } from "@/components/AnsiText";
import { levelOf, type Level, type Parsed } from "@/lib/logline";
import { cn } from "@/lib/utils";

const badge: Record<Level, string> = {
  trace: "bg-zinc-800 text-zinc-400",
  debug: "bg-zinc-800 text-zinc-300",
  info: "bg-sky-500/10 text-sky-300",
  warn: "bg-amber-500/15 text-amber-300",
  error: "bg-rose-500/15 text-rose-300",
  fatal: "bg-rose-600/30 text-rose-200",
};

const textColor: Partial<Record<Level, string>> = {
  trace: "text-zinc-500",
  debug: "text-zinc-400",
  warn: "text-amber-200",
  error: "text-rose-300",
  fatal: "text-rose-200 font-semibold",
};

/** Row tint for lines that need attention, so they stand out in a long scroll. */
export const rowTint = (p: Parsed): string => {
  const l = levelOf(p);
  return l === "error" || l === "fatal" ? "bg-rose-500/[0.07]" : l === "warn" ? "bg-amber-500/[0.05]" : "";
};

function LevelBadge({ level }: { level: Level }) {
  return <span className={cn("mr-2 inline-block w-[3.75rem] rounded px-1.5 text-center text-xs font-semibold uppercase tracking-wide", badge[level])}>{level}</span>;
}

const statusColor = (s: number) => (s >= 500 ? "text-rose-300" : s >= 400 ? "text-amber-300" : s >= 300 ? "text-cyan-300" : "text-emerald-300");

/**
 * One log line, laid out for reading. The original text is untouched elsewhere (raw view, download); this
 * only changes how it is drawn. Nothing is ever interpreted as HTML.
 */
export function LogBody({ raw, parsed }: { raw: string; parsed: Parsed }) {
  switch (parsed.kind) {
    case "structured":
      return (
        <span className="min-w-0">
          {parsed.level && <LevelBadge level={parsed.level} />}
          <span className={cn("break-words", (parsed.level && textColor[parsed.level]) || "text-zinc-100")}>{parsed.message || <span className="text-zinc-500">(no message)</span>}</span>
          {parsed.fields.length > 0 && (
            <span className="ml-2 break-words text-zinc-500">
              {parsed.fields.map(([k, v], i) => (
                <span key={`${k}-${i}`} className="mr-3 inline-block">
                  <span className="text-zinc-500">{k}</span>
                  <span className="text-zinc-600">=</span>
                  <span className="text-zinc-300">{v}</span>
                </span>
              ))}
            </span>
          )}
        </span>
      );
    case "access":
      return (
        <span className="min-w-0 break-all">
          <span className="mr-2 inline-block w-14 font-semibold text-zinc-300">{parsed.method}</span>
          <span className="text-zinc-100">{parsed.path}</span>
          <span className={cn("ml-3 font-semibold", statusColor(parsed.status))}>{parsed.status}</span>
          {parsed.bytes && <span className="ml-3 text-zinc-500">{parsed.bytes} B</span>}
          {parsed.client && <span className="ml-3 text-zinc-600">{parsed.client}</span>}
        </span>
      );
    case "trace":
      return (
        <span className="min-w-0 break-all border-l border-zinc-800 pl-3 text-zinc-500">
          <AnsiText text={raw} />
        </span>
      );
    case "text":
      return (
        <span className={cn("min-w-0 whitespace-pre-wrap break-words", (parsed.level && textColor[parsed.level]) || "text-zinc-200")}>
          {parsed.time && <span className="mr-2 text-zinc-600">{parsed.time}</span>}
          {parsed.level && !/^\s*[[(]?(fatal|critical|panic|error|err|warn|warning|info|debug|trace)/i.test(parsed.rest) && <LevelBadge level={parsed.level} />}
          <AnsiText text={parsed.rest} />
        </span>
      );
  }
}
