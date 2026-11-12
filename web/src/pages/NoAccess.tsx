import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Anchor, LogOut, RefreshCw, UserRound } from "lucide-react";
import { api, type Me } from "@/lib/api";
import { Button } from "@/components/ui/button";

/** Shown to someone who signed in but has no role yet. It checks again by itself, so access appears as soon as it is granted. */
export default function NoAccess({ me }: { me: Me }) {
  const qc = useQueryClient();
  useEffect(() => {
    const t = setInterval(() => void qc.invalidateQueries({ queryKey: ["me"] }), 10_000);
    return () => clearInterval(t);
  }, [qc]);

  // Signing out first, then asking GitHub for its account picker, so another account can be chosen.
  const switchAccount = async () => {
    await api("/auth/logout", { method: "POST" }).catch(() => undefined);
    qc.clear();
    window.location.assign("/auth/github/login?switch=1");
  };
  const signOut = async () => {
    await api("/auth/logout", { method: "POST" }).catch(() => undefined);
    qc.clear();
    window.location.assign("/login");
  };

  return (
    <main className="flex min-h-screen items-center justify-center bg-black px-4 text-zinc-100" aria-labelledby="no-access-title">
      <div className="w-full max-w-md space-y-6 rounded-2xl bg-[#0a0a0d] p-8">
        <div className="flex items-center gap-2.5 text-sm font-bold tracking-tight text-white">
          <Anchor className="size-5" strokeWidth={2} />
          Helmsman
        </div>

        <div className="flex items-center gap-3 rounded-lg bg-[#121215] p-3">
          {me.user.avatar_url ? <img src={me.user.avatar_url} alt="" className="size-9 rounded-full" /> : <UserRound className="size-9 rounded-full bg-zinc-800 p-2 text-zinc-400" strokeWidth={1.5} />}
          <div className="min-w-0">
            <div className="truncate text-sm font-medium text-white">{me.user.name || me.user.login}</div>
            <div className="truncate font-mono text-xs text-zinc-500">@{me.user.login}</div>
          </div>
        </div>

        <div className="space-y-2">
          <h1 id="no-access-title" className="text-xl font-semibold tracking-tight text-white">
            You don't have access yet
          </h1>
          <p className="text-sm leading-relaxed text-zinc-400">
            You are signed in, but no role has been given to you, so there is nothing to show yet. Your request is with the admins, and an admin will grant you access.
          </p>
          <p className="flex items-center gap-2 text-xs text-zinc-500">
            <RefreshCw className="size-3.5" strokeWidth={1.5} />
            This page checks again by itself, so you do not need to reload.
          </p>
        </div>

        <div className="flex flex-wrap gap-2.5">
          <Button onClick={() => void switchAccount()}>Switch account</Button>
          <Button variant="ghost" onClick={() => void signOut()}>
            <LogOut className="size-3.5" strokeWidth={1.5} />
            Sign out
          </Button>
        </div>
        <p className="text-xs text-zinc-600">Switching signs you out here and lets you pick another GitHub account. Your request stays with the admins.</p>
      </div>
    </main>
  );
}
