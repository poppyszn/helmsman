import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Mail as MailIcon, Paperclip, Search } from "lucide-react";
import { api, formatTime, mailAddr, useMe, type MailList, type MailMessage } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

// The email's own HTML is shown in a frame with no scripts and no access to Helmsman: the sandbox has no
// allow-scripts or allow-same-origin, and the policy blocks every network load (remote images, trackers).
// Links open in a new tab.
const frameDoc = (html: string) =>
  `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:"><base target="_blank"><style>body{margin:16px;font-family:system-ui,sans-serif;font-size:14px;line-height:1.5;color:#111;background:#fff}</style>${html}`;

function useDebounced(v: string, ms: number) {
  const [d, setD] = useState(v);
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms);
    return () => clearTimeout(t);
  }, [v, ms]);
  return d;
}

export default function Mail() {
  const { data: me } = useMe();
  const tz = me?.display_tz ?? "UTC";
  const [params, setParams] = useSearchParams();
  const selected = params.get("m");
  const [search, setSearch] = useState("");
  const query = useDebounced(search.trim(), 300);

  const list = useQuery<MailList>({
    queryKey: ["mail", query],
    queryFn: () => api(`/api/mail/messages?limit=50${query ? `&q=${encodeURIComponent(query)}` : ""}`),
    refetchInterval: 5_000,
  });
  const l = list.data;

  if (list.isLoading) return <div className="py-12 text-center text-xs font-mono text-zinc-500">loading</div>;
  if (list.isError || !l) return <div className="py-12 text-center text-xs font-mono text-rose-300">could not load the mail</div>;

  if (!l.connected) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight text-white">Mail</h1>
        <Card className="space-y-2 p-8 text-center">
          <MailIcon className="mx-auto size-6 text-zinc-500" strokeWidth={1.5} />
          <p className="text-sm text-zinc-300">Mail is not connected yet.</p>
          {me?.can.admin ? (
            <p className="text-sm text-zinc-400">
              Connect a Mailpit on the{" "}
              <Link to="/admin/integrations" className="text-sky-400 underline underline-offset-4 hover:text-sky-300">
                Integrations page
              </Link>{" "}
              and the messages your development environments send will show up here.
            </p>
          ) : (
            <p className="text-sm text-zinc-400">Ask an admin to connect Mailpit on the Integrations page.</p>
          )}
        </Card>
      </div>
    );
  }

  const open = (id: string) => {
    const n = new URLSearchParams(params);
    n.set("m", id);
    setParams(n, { replace: true });
  };

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Mail</h1>
          <p className="mt-1 text-sm text-zinc-400">
            {l.total} {l.total === 1 ? "message" : "messages"}
            {l.unread > 0 && <span className="text-zinc-500"> · {l.unread} unread</span>}. Sent by your development environments, read-only.
          </p>
        </div>
        <div className="relative w-full sm:w-80">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-zinc-500" strokeWidth={1.5} />
          <Input
            className="pl-9"
            placeholder="Search, or to:alice@example.com"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label="Search mail"
          />
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[24rem_minmax(0,1fr)]">
        <Card className="max-h-[calc(100vh-13rem)] overflow-y-auto p-0">
          {l.messages.length === 0 ? (
            <div className="px-4 py-12 text-center text-xs font-mono text-zinc-500">{query ? "no matching messages" : "no mail yet"}</div>
          ) : (
            <ul>
              {l.messages.map((m) => (
                <li key={m.ID}>
                  <button
                    type="button"
                    onClick={() => open(m.ID)}
                    aria-current={selected === m.ID}
                    className={cn(
                      "block w-full border-t border-zinc-900/60 px-4 py-3 text-left first:border-t-0 transition-colors hover:bg-[#121216]",
                      selected === m.ID && "bg-[#141418]",
                    )}
                  >
                    <div className="flex items-center gap-2">
                      {!m.Read && <span className="size-2 shrink-0 rounded-full bg-sky-400" aria-label="unread" />}
                      <span className={cn("min-w-0 flex-1 truncate text-sm", m.Read ? "text-zinc-300" : "font-semibold text-white")}>{m.From.Name || m.From.Address}</span>
                      <span className="shrink-0 text-xs font-mono text-zinc-500">{formatTime(m.Created, tz)}</span>
                    </div>
                    <div className={cn("mt-0.5 truncate text-sm", m.Read ? "text-zinc-400" : "text-zinc-200")}>{m.Subject || "(no subject)"}</div>
                    <div className="mt-0.5 flex items-center gap-2 text-xs text-zinc-500">
                      <span className="min-w-0 flex-1 truncate">to {(m.To ?? []).map((a) => a.Address).join(", ") || "nobody"}</span>
                      {m.Attachments > 0 && <Paperclip className="size-3.5 shrink-0" strokeWidth={1.5} aria-label="has attachments" />}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <div className="min-w-0">{selected ? <MessageView id={selected} tz={tz} /> : <Card className="py-16 text-center text-sm text-zinc-500">Choose a message to read it.</Card>}</div>
      </div>
    </div>
  );
}

function MessageView({ id, tz }: { id: string; tz: string }) {
  const q = useQuery<{ message: MailMessage }>({ queryKey: ["mail-message", id], queryFn: () => api(`/api/mail/messages/${encodeURIComponent(id)}`), retry: false, staleTime: Infinity });
  const [tab, setTab] = useState<"html" | "text">("html");
  if (q.isLoading) return <Card className="py-16 text-center text-xs font-mono text-zinc-500">loading</Card>;
  if (q.isError || !q.data) return <Card className="py-16 text-center text-sm text-rose-300">This message could not be loaded. It may have been deleted in Mailpit.</Card>;
  const m = q.data.message;
  const hasHTML = m.HTML.trim() !== "";
  const view = hasHTML ? tab : "text";
  const people = (label: string, a: { Name: string; Address: string }[] | null) =>
    a && a.length > 0 ? (
      <div className="flex gap-2">
        <span className="w-8 shrink-0 text-zinc-500">{label}</span>
        <span className="min-w-0 break-words text-zinc-300">{a.map(mailAddr).join(", ")}</span>
      </div>
    ) : null;
  return (
    <Card className="space-y-4 p-5">
      <div>
        <h2 className="text-lg font-semibold tracking-tight text-white break-words">{m.Subject || "(no subject)"}</h2>
        <div className="mt-3 space-y-1 text-xs font-mono">
          <div className="flex gap-2">
            <span className="w-8 shrink-0 text-zinc-500">from</span>
            <span className="min-w-0 break-words text-zinc-300">{mailAddr(m.From)}</span>
          </div>
          {people("to", m.To)}
          {people("cc", m.Cc)}
          {people("bcc", m.Bcc)}
          <div className="flex gap-2">
            <span className="w-8 shrink-0 text-zinc-500">date</span>
            <span className="text-zinc-400">{formatTime(m.Date, tz)}</span>
          </div>
        </div>
      </div>

      {(m.Attachments ?? []).length > 0 && (
        <div className="flex flex-wrap gap-2">
          {m.Attachments!.map((a) => (
            <a
              key={a.PartID}
              href={`/api/mail/messages/${encodeURIComponent(m.ID)}/attachments/${encodeURIComponent(a.PartID)}`}
              className="inline-flex items-center gap-1.5 rounded-md bg-[#141418] px-2.5 py-1.5 text-xs font-mono text-sky-400 hover:bg-[#1a1a22] hover:text-sky-300"
              title={`${a.ContentType}, ${a.Size} bytes. Downloads, it is never opened here.`}
            >
              <Paperclip className="size-3.5" strokeWidth={1.5} />
              {a.FileName || `part ${a.PartID}`}
            </a>
          ))}
        </div>
      )}

      {hasHTML && (
        <div role="tablist" aria-label="Message format" className="flex items-center gap-5 border-b border-zinc-900 pb-2 text-sm font-mono">
          {(["html", "text"] as const).map((t) => (
            <button
              key={t}
              role="tab"
              aria-selected={view === t}
              onClick={() => setTab(t)}
              className={cn("pb-1 transition-colors", view === t ? "border-b-2 border-white text-white" : "text-zinc-500 hover:text-zinc-300")}
            >
              {t}
            </button>
          ))}
        </div>
      )}

      {view === "html" ? (
        <iframe
          title="Message body"
          sandbox="allow-popups allow-popups-to-escape-sandbox"
          srcDoc={frameDoc(m.HTML)}
          className="h-[60vh] w-full rounded-lg bg-white"
        />
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded-lg bg-[#060608] p-4 text-xs font-mono leading-relaxed text-zinc-300 whitespace-pre-wrap break-words">{m.Text || "(this message has no text)"}</pre>
      )}
    </Card>
  );
}
