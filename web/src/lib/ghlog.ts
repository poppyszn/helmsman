// Turns the raw text of a GitHub Actions job log into sections a person can scan: each
// "##[group]" block (one per step command) becomes a titled section, and errors and warnings
// are marked. Timestamps and colour codes are removed.

export type LogLine = { text: string; kind: "plain" | "error" | "warning" };
export type LogSection = { title: string | null; lines: LogLine[]; hasError: boolean };

const STAMP = /^\ufeff?\d{4}-\d{2}-\d{2}T[\d:.]+Z ?/;
// eslint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;]*[A-Za-z]/g;

export function parseJobLog(raw: string): LogSection[] {
  const sections: LogSection[] = [];
  let cur: LogSection | null = null;
  const open = (title: string | null) => {
    cur = { title, lines: [], hasError: false };
    sections.push(cur);
    return cur;
  };
  for (const rawLine of raw.split("\n")) {
    const line = rawLine.replace(/\r$/, "").replace(STAMP, "").replace(ANSI, "");
    if (line.startsWith("##[group]")) {
      open(line.slice("##[group]".length) || "(step)");
      continue;
    }
    if (line.startsWith("##[endgroup]")) {
      cur = null;
      continue;
    }
    if (line === "" && !cur) continue;
    const sec: LogSection = cur ?? (sections.length > 0 && sections[sections.length - 1].title === null ? sections[sections.length - 1] : open(null));
    if (line.startsWith("##[error]")) {
      sec.lines.push({ text: line.slice("##[error]".length), kind: "error" });
      sec.hasError = true;
    } else if (line.startsWith("##[warning]")) {
      sec.lines.push({ text: line.slice("##[warning]".length), kind: "warning" });
    } else {
      sec.lines.push({ text: line, kind: "plain" });
    }
  }
  return sections;
}

/** Time between two timestamps. Without an end it counts up to now, but only for work still running. */
export function durationLabel(start?: string | null, end?: string | null, running = false): string {
  if (!start || (!end && !running)) return "";
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  const sec = Math.max(0, Math.round((e - s) / 1000));
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  return `${m}m ${String(sec % 60).padStart(2, "0")}s`;
}
