import { useSearchParams } from "react-router-dom";
import { Anchor } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

function message(code: string | null, org: string | null): string | null {
  switch (code) {
    case null:
      return null;
    case "not_in_org":
      return "Your GitHub account is not in an organization that may use Helmsman.";
    case "org_restricted":
      return `The GitHub organization ${org ?? ""} restricts third-party OAuth apps and has not approved Helmsman. Ask an owner of ${org ?? "that organization"} to approve it, then try again.`;
    case "github_unavailable":
      return "GitHub is not reachable right now, so new sign-ins are paused. Try again in a few minutes.";
    case "disabled":
      return "This account is disabled. Ask an administrator.";
    case "state":
      return "The sign-in attempt expired or was tampered with. Start again.";
    case "denied":
      return "GitHub sign-in was cancelled.";
    default:
      return "Sign-in failed. Try again.";
  }
}

export default function Login() {
  const [params] = useSearchParams();
  const msg = message(params.get("error"), params.get("org"));
  return (
    <div className="mx-auto mt-24 max-w-sm px-4">
      <Card className="space-y-4 p-6">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <Anchor className="size-5" aria-hidden /> Helmsman
        </div>
        {msg && (
          <p role="alert" className="rounded border border-red-500/40 p-3 text-sm text-red-600 dark:text-red-400">
            {msg}
          </p>
        )}
        <Button asChild className="w-full">
          <a href="/auth/github/login">Sign in with GitHub</a>
        </Button>
      </Card>
    </div>
  );
}
