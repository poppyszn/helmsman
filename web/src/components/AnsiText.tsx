import { Fragment } from "react";

// Renders log text safely: ANSI colour codes become classed spans, every other escape
// sequence is dropped, and nothing is ever interpreted as HTML (React escapes text nodes).
const FG: Record<number, string> = {
  30: "text-neutral-500", 31: "text-red-500", 32: "text-green-500", 33: "text-amber-500",
  34: "text-blue-500", 35: "text-fuchsia-500", 36: "text-cyan-500", 37: "text-neutral-300",
  90: "text-neutral-500", 91: "text-red-400", 92: "text-green-400", 93: "text-amber-400",
  94: "text-blue-400", 95: "text-fuchsia-400", 96: "text-cyan-400", 97: "text-neutral-100",
};

// eslint-disable-next-line no-control-regex
const ESC = /\u001b\[([0-9;]*)m|\u001b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\u0007\u001b]*(?:\u0007|\u001b\\)|[@-Z\\-_])/g;
// eslint-disable-next-line no-control-regex
const CTRL = /[\u0000-\u0008\u000b-\u001a\u001c-\u001f\u007f]/g;

export function parseAnsi(input: string): { text: string; cls: string }[] {
  const out: { text: string; cls: string }[] = [];
  let cls = "";
  let bold = false;
  let last = 0;
  const push = (t: string) => {
    const clean = t.replace(CTRL, "\uFFFD");
    if (clean) out.push({ text: clean, cls: `${cls}${bold ? " font-bold" : ""}`.trim() });
  };
  for (const m of input.matchAll(ESC)) {
    push(input.slice(last, m.index));
    last = m.index + m[0].length;
    if (m[1] === undefined) continue; // some other escape sequence: dropped
    for (const code of (m[1] || "0").split(";").map(Number)) {
      if (code === 0) { cls = ""; bold = false; }
      else if (code === 1) bold = true;
      else if (code === 22) bold = false;
      else if (code === 39) cls = "";
      else if (FG[code]) cls = FG[code];
    }
  }
  push(input.slice(last));
  return out;
}

export function AnsiText({ text }: { text: string }) {
  return (
    <>
      {parseAnsi(text).map((p, i) => (
        <Fragment key={i}>{p.cls ? <span className={p.cls}>{p.text}</span> : p.text}</Fragment>
      ))}
    </>
  );
}
