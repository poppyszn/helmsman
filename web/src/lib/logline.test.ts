import { describe, expect, it } from "vitest";
import { levelOf, normalizeLevel, parseLogLine } from "./logline";

describe("parseLogLine", () => {
  it("reads a pino JSON line with a numeric level", () => {
    const p = parseLogLine('{"level":50,"time":1700000000000,"pid":1,"msg":"db down","err":{"code":"ECONNREFUSED"}}');
    expect(p.kind).toBe("structured");
    if (p.kind !== "structured") return;
    expect(p.level).toBe("error");
    expect(p.message).toBe("db down");
    expect(p.fields.map(([k]) => k)).toEqual(["pid", "err"]);
    expect(p.fields[1][1]).toContain("ECONNREFUSED");
  });

  it("reads a winston JSON line with a string level", () => {
    const p = parseLogLine('{"level":"warn","message":"slow query","timestamp":"2026-11-12T10:00:00Z","ms":1200}');
    expect(p).toMatchObject({ kind: "structured", level: "warn", message: "slow query", time: "2026-11-12T10:00:00Z" });
  });

  it("reads Elastic ECS style keys", () => {
    const p = parseLogLine('{"@timestamp":"2026-11-12T10:00:00Z","log.level":"info","message":"started"}');
    expect(p).toMatchObject({ kind: "structured", level: "info", message: "started" });
  });

  it("leaves JSON that is data, not a log line, as text", () => {
    expect(parseLogLine('{"user":1,"items":[1,2]}').kind).toBe("text");
    expect(parseLogLine("[1,2,3]").kind).toBe("text");
    expect(parseLogLine("{not json}").kind).toBe("text");
  });

  it("reads logfmt", () => {
    const p = parseLogLine('time=2026-11-12T10:00:00Z level=error msg="could not connect" host=db1 attempt=3');
    expect(p).toMatchObject({ kind: "structured", format: "logfmt", level: "error", message: "could not connect" });
    if (p.kind === "structured") expect(p.fields).toEqual([["host", "db1"], ["attempt", "3"]]);
  });

  it("does not mistake prose with an equals sign for logfmt", () => {
    expect(parseLogLine("Retrying with a=1 and b=2 after the failure").kind).toBe("text");
  });

  it("reads an nginx access line and colours by status", () => {
    const line = '10.0.0.5 - - [12/Nov/2026:10:00:00 +0000] "GET /api/health HTTP/1.1" 503 12 "-" "curl/8"';
    const p = parseLogLine(line);
    expect(p).toMatchObject({ kind: "access", method: "GET", path: "/api/health", status: 503, bytes: "12", client: "10.0.0.5" });
    expect(levelOf(p)).toBe("error");
    expect(levelOf(parseLogLine(line.replace("503", "404")))).toBe("warn");
    expect(levelOf(parseLogLine(line.replace("503", "200")))).toBeUndefined();
  });

  it("recognises stack trace lines of common languages", () => {
    for (const l of [
      "    at Object.<anonymous> (/app/index.js:10:5)",
      'Traceback (most recent call last):',
      '  File "/app/main.py", line 3, in <module>',
      "Caused by: java.io.IOException: closed",
      "\t/app/server.go:42 +0x1d",
      "goroutine 1 [running]:",
    ]) expect(parseLogLine(l).kind, l).toBe("trace");
  });

  it("finds a level in plain text and strips the leading timestamp", () => {
    const p = parseLogLine("2026-11-12 10:00:00,123 ERROR failed to send mail");
    expect(p).toMatchObject({ kind: "text", level: "error", time: "2026-11-12 10:00:00,123", rest: "ERROR failed to send mail" });
    expect(parseLogLine("2026/11/12 10:00:00 [warn] 29#29: upstream slow")).toMatchObject({ kind: "text", level: "warn" });
    expect(parseLogLine("[INFO] listening on :8080")).toMatchObject({ level: "info" });
  });

  it("does not turn an ordinary word into a level", () => {
    expect(parseLogLine("the error handler was registered")).toMatchObject({ kind: "text", level: undefined });
    expect(parseLogLine("information about errors")).toMatchObject({ level: undefined });
  });

  it("keeps ANSI colour codes in the text and still finds the level", () => {
    const p = parseLogLine("\u001b[31mERROR\u001b[0m boom");
    expect(p).toMatchObject({ kind: "text", level: "error" });
    if (p.kind === "text") expect(p.rest).toContain("\u001b[31m");
  });

  it("never throws on odd input", () => {
    for (const l of ["", " ", "{", "}", "{}", '{"level":', "\u0000\u0001", "x".repeat(100000)]) expect(() => parseLogLine(l)).not.toThrow();
  });
});

describe("normalizeLevel", () => {
  it("maps names and numbers", () => {
    expect(normalizeLevel("WARNING")).toBe("warn");
    expect(normalizeLevel("Critical")).toBe("fatal");
    expect(normalizeLevel(30)).toBe("info");
    expect(normalizeLevel(60)).toBe("fatal");
    expect(normalizeLevel("nonsense")).toBeUndefined();
    expect(normalizeLevel(undefined)).toBeUndefined();
  });
});
