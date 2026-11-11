import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, formatTime, useMe } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";

type S = { path: string; rev: string; loaded_at: string | null; apps: number; last_error: string; last_error_at: string | null };

export default function Catalog() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const { data } = useQuery<S>({ queryKey: ["catalog"], queryFn: () => api("/api/admin/catalog"), refetchInterval: 10000 });
  const reload = useMutation({
    mutationFn: () => api("/api/admin/catalog/reload", { method: "POST" }),
    onSettled: () => { qc.invalidateQueries({ queryKey: ["catalog"] }); qc.invalidateQueries({ queryKey: ["apps"] }); },
  });
  if (!data || !me) return <p className="text-sm text-muted">Loading</p>;
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Catalog</h1>
      <Card className="space-y-2 text-sm">
        <div>File: <code>{data.path}</code></div>
        <div>Active revision: <code>{data.rev || "none loaded"}</code> ({data.apps} apps)</div>
        <div>Loaded: {formatTime(data.loaded_at, me.display_tz)}</div>
        <Button size="sm" onClick={() => reload.mutate()} disabled={reload.isPending}>Reload now</Button>
      </Card>
      {data.last_error && (
        <Card className="border-red-500/50">
          <h2 className="mb-1 font-medium text-red-600 dark:text-red-400">The last reload was rejected</h2>
          <p className="mb-2 text-xs text-muted">
            The last good catalog stays active. Failed at {formatTime(data.last_error_at, me.display_tz)}.
          </p>
          <pre className="whitespace-pre-wrap text-xs">{data.last_error}</pre>
        </Card>
      )}
    </div>
  );
}
