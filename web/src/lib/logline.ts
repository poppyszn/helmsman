// Works out what a log line is, whatever the app wrote: a JSON object, key=value pairs, an nginx access
// line, a stack trace line, or plain text. The viewer uses it to show a level, the message and the fields
// cleanly; the original text is always still available (the raw view and downloads never change).

export type Level = "trace" | "debug" | "info" | "warn" | "error" | "fatal";
export const LEVEL_ORDER: Level[] = ["trace", "debug", "info", "warn", "error", "fatal"];
export const levelRank = (l: Level | undefined) => (l ? LEVEL_ORDER.indexOf(l) : -1);

export type Field = [key: string, value: string];

export type Parsed =
  | { kind: "structured"; format: "json" | "logfmt"; level?: Level; message: string; time?: string; fields: Field[] }
  | { kind: "access"; method: string; path: string; status: number; bytes?: string; client?: string; agent?: string }
  | { kind: "trace" }
  | { kind: "text"; level?: Level; time?: string; rest: string };

const LEVEL_KEYS = ["level", "lvl", "severity", "log.level", "loglevel"];
const MESSAGE_KEYS = ["msg", "message", "event", "log"];
const TIME_KEYS = ["time", "timestamp", "ts", "@timestamp", "t", "datetime"];

/** A level name or number from the common logging libraries, or undefined when it is not one. */
export function normalizeLevel(v: unknown): Level | undefined {
  if (typeof v === "number") {
    // pino and bunyan: 10 trace, 20 debug, 30 info, 40 warn, 50 error, 60 fatal
    if (v <= 10) return "trace";
    if (v <= 20) return "debug";
    if (v <= 30) return "info";
    if (v <= 40) return "warn";
    if (v <= 50) return "error";
    return "fatal";
  }
  if (typeof v !== "string") return undefined;
  switch (v.trim().toLowerCase()) {
    case "trace":
    case "verbose":
    case "silly":
      return "trace";
    case "debug":
    case "dbg":
      return "debug";
    case "info":
    case "information":
    case "notice":
    case "log":
      return "info";
    case "warn":
    case "warning":
      return "warn";
    case "error":
    case "err":
    case "severe":
      return "error";
    case "fatal":
    case "critical":
    case "crit":
    case "panic":
    case "alert":
    case "emerg":
    case "emergency":
      return "fatal";
  }
  return undefined;
}

// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*[A-Za-z]/g;
const LEADING_TIME = /^(\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?)\s+/;
const ACCESS = /^(\S+) \S+ \S+ \[[^\]]+\] "([A-Z]+) (\S+)[^"]*" (\d{3}) (\d+|-)(?: "[^"]*" "([^"]*)")?/;
const TRACE_LINE = /^(\s+at\s|\s+File "|Traceback \(most recent call last\)|\s*\.\.\. \d+ more|Caused by:|goroutine \d+ \[|\s+\S+\.(go|py|rb|java|js|ts):\d+|\t)/;

const MAX_PATTERN_LINE = 4096;

function truncate(s: string, n = 200): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function show(v: unknown): string {
  if (typeof v === "string") return truncate(v);
  if (v === null || typeof v === "number" || typeof v === "boolean") return String(v);
  try {
    return truncate(JSON.stringify(v));
  } catch {
    return String(v);
  }
}

function pick(obj: Record<string, unknown>, keys: string[]): [string, unknown] | undefined {
  for (const k of keys) if (k in obj && obj[k] !== undefined) return [k, obj[k]];
  return undefined;
}

function fromObject(obj: Record<string, unknown>, format: "json" | "logfmt"): Parsed | undefined {
  const lv = pick(obj, LEVEL_KEYS);
  const msg = pick(obj, MESSAGE_KEYS);
  const tm = pick(obj, TIME_KEYS);
  // A JSON object that has neither a level nor a message is data, not a log line: leave it as text.
  if (!lv && !msg) return undefined;
  const used = new Set([lv?.[0], msg?.[0], tm?.[0]].filter(Boolean) as string[]);
  const fields: Field[] = Object.entries(obj)
    .filter(([k]) => !used.has(k))
    .map(([k, v]) => [k, show(v)] as Field);
  return {
    kind: "structured",
    format,
    level: normalizeLevel(lv?.[1]),
    message: msg ? (typeof msg[1] === "string" ? msg[1] : show(msg[1])) : "",
    time: tm ? String(tm[1]) : undefined,
    fields,
  };
}

const PAIR = /([A-Za-z_][\w.@-]*)=("(?:[^"\\]|\\.)*"|\S*)/g;

function parseLogfmt(line: string): Parsed | undefined {
  const pairs: Record<string, unknown> = {};
  let n = 0;
  for (const m of line.matchAll(PAIR)) {
    let v: string = m[2];
    if (v.startsWith('"') && v.endsWith('"') && v.length >= 2) {
      try {
        v = JSON.parse(v) as string;
      } catch {
        v = v.slice(1, -1);
      }
    }
    pairs[m[1]] = v;
    n++;
  }
  if (n < 2) return undefined;
  // Real logfmt starts with its pairs (maybe after a timestamp), it is not prose that mentions a=b.
  const stripped = line.replace(LEADING_TIME, "");
  if (!/^[A-Za-z_][\w.@-]*=/.test(stripped)) return undefined;
  return fromObject(pairs, "logfmt");
}

/** Looks for a level word in plain text: ERROR, [warn], level=error, "level":"info". */
export function levelInText(text: string): Level | undefined {
  const head = text.slice(0, 120);
  const kv = head.match(/\b(?:level|lvl|severity)["']?\s*[=:]\s*["']?([A-Za-z]+)/i);
  if (kv) {
    const l = normalizeLevel(kv[1]);
    if (l) return l;
  }
  const bracket = head.match(/\[(emerg|alert|crit|error|err|warn|warning|notice|info|debug|trace|fatal)\]/i);
  if (bracket) return normalizeLevel(bracket[1]);
  const bare = head.match(/(?:^|[\s|])(FATAL|CRITICAL|PANIC|ERROR|WARNING|WARN|INFO|DEBUG|TRACE)\b/);
  if (bare) return normalizeLevel(bare[1]);
  return undefined;
}

/** Classifies one line. It never throws and never changes the text. */
export function parseLogLine(raw: string): Parsed {
  const text = raw.replace(ANSI, "");
  const t = text.trim();
  if (t.startsWith("{") && t.endsWith("}")) {
    try {
      const v = JSON.parse(t) as unknown;
      if (v && typeof v === "object" && !Array.isArray(v)) {
        const p = fromObject(v as Record<string, unknown>, "json");
        if (p) return p;
      }
    } catch {
      /* not JSON after all */
    }
  }
  // The patterns below can backtrack badly on a very long line, and a line that long is a dump, not a log
  // entry: show it as text, finding the level from its start only.
  if (text.length > MAX_PATTERN_LINE) return { kind: "text", level: levelInText(text), rest: raw };
  const acc = ACCESS.exec(text);
  if (acc) return { kind: "access", client: acc[1], method: acc[2], path: acc[3], status: Number(acc[4]), bytes: acc[5] === "-" ? undefined : acc[5], agent: acc[6] };
  if (TRACE_LINE.test(text)) return { kind: "trace" };
  const kv = parseLogfmt(text);
  if (kv) return kv;
  const time = LEADING_TIME.exec(raw);
  const rest = time ? raw.slice(time[0].length) : raw;
  return { kind: "text", level: levelInText(rest.replace(ANSI, "")), time: time?.[1], rest };
}

/** The level of a parsed line, if it has one. */
export function levelOf(p: Parsed): Level | undefined {
  if (p.kind === "structured" || p.kind === "text") return p.level;
  if (p.kind === "access") return p.status >= 500 ? "error" : p.status >= 400 ? "warn" : undefined;
  return undefined;
}
