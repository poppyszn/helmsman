import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Anchor } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { api, ApiError } from "@/lib/api";

function GithubIcon({ className = "size-4" }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.53 1.032 1.53 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"
      />
    </svg>
  );
}

function message(code: string | null, org: string | null): string | null {
  switch (code) {
    case null:
      return null;
    case "not_in_org":
      return "Your GitHub account is not in an organization that may use Helmsman.";
    case "org_restricted":
      return `The GitHub organization ${org ?? ""} restricts third-party OAuth apps and has not approved Helmsman.`;
    case "github_unavailable":
      return "GitHub is not reachable right now.";
    case "disabled":
      return "This account is disabled.";
    case "state":
      return "The sign-in attempt expired. Start again.";
    case "denied":
      return "Sign-in was cancelled.";
    default:
      return "Sign-in failed. Try again.";
  }
}

function Breakglass() {
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (!open) {
    return (
      <button onClick={() => setOpen(true)} className="text-xs font-mono text-zinc-500 hover:text-zinc-300 cursor-pointer">
        Emergency access
      </button>
    );
  }
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await api("/auth/breakglass", { method: "POST", json: { password, code } });
      window.location.href = "/";
    } catch (x) {
      setErr(x instanceof ApiError ? x.message : "sign-in failed");
      setBusy(false);
    }
  };
  return (
    <form onSubmit={submit} className="space-y-3 rounded-xl bg-[#0a0a0d] p-5 text-left">
      <p className="text-xs text-zinc-400 font-mono">Local admin sign-in. Every attempt is audited.</p>
      {err && <p role="alert" className="rounded-lg bg-rose-500/10 p-3 text-xs text-rose-300 font-mono">{err}</p>}
      <label className="block text-xs font-mono text-zinc-400">
        Password
        <Input type="password" autoComplete="off" className="mt-1.5 bg-black" value={password} onChange={(e) => setPassword(e.target.value)} />
      </label>
      <label className="block text-xs font-mono text-zinc-400">
        Authenticator code
        <Input inputMode="numeric" autoComplete="one-time-code" maxLength={6} className="mt-1.5 bg-black font-mono" value={code} onChange={(e) => setCode(e.target.value)} />
      </label>
      <Button type="submit" variant="secondary" className="w-full" disabled={busy || !password || code.length !== 6}>
        Sign in
      </Button>
    </form>
  );
}

export default function Login() {
  const opts = useQuery<{ breakglass: boolean }>({ queryKey: ["auth-options"], queryFn: () => api("/auth/options"), retry: false });
  const [params] = useSearchParams();
  const msg = message(params.get("error"), params.get("org"));

  return (
    <div className="flex min-h-screen items-center justify-center bg-black px-4 font-sans antialiased">
      <div className="w-full max-w-sm space-y-5 text-center">
        <div className="space-y-2">
          <Anchor className="size-7 text-white mx-auto mb-3" strokeWidth={2} />
          <h1 className="text-xl font-bold tracking-tight text-white">Helmsman</h1>
          <p className="text-xs text-zinc-400">Internal Developer Portal</p>
        </div>

        <Card className="p-6 space-y-4 bg-[#0a0a0d]">
          {msg && (
            <div
              role="alert"
              className="rounded-lg bg-rose-500/10 p-3 text-xs text-rose-300 font-mono text-left"
            >
              {msg}
            </div>
          )}

          <Button asChild className="w-full h-10 text-sm font-semibold">
            <a href="/auth/github/login" className="flex items-center justify-center gap-2.5">
              <GithubIcon className="size-4" />
              <span>Sign in with GitHub</span>
            </a>
          </Button>
        </Card>
        {opts.data?.breakglass && <Breakglass />}
      </div>
    </div>
  );
}
