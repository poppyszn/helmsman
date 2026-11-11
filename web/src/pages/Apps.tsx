import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, type App } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

export default function Apps() {
  const { data, isPending, error } = useQuery<App[]>({ queryKey: ["apps"], queryFn: () => api("/api/apps") });
  if (isPending) return <p className="text-sm text-muted">Loading</p>;
  if (error) {
    return (
      <Card>
        <p className="text-sm">You do not have access to any apps yet. Ask an administrator to grant you a role.</p>
      </Card>
    );
  }
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Apps</h1>
      {data.length === 0 && (
        <Card><p className="text-sm text-muted">No apps visible to you.</p></Card>
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        {data.map((a) => (
          <Link key={a.id} to={`/apps/${a.id}`}>
            <Card className="hover:border-primary">
              <div className="font-medium">{a.name}</div>
              <div className="mb-2 text-xs text-muted">{a.repo}</div>
              <div className="flex flex-wrap gap-1.5">
                {a.environments.map((e) => (
                  <Badge key={e.env} tone={e.tier === "prod" ? "prod" : "default"}>{e.env}</Badge>
                ))}
              </div>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  );
}
