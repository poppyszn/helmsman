import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { api, formatTime, useMe } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";

type CatalogStatus = {
  path: string;
  rev: string;
  loaded_at: string | null;
  apps: number;
  last_error: string;
  last_error_at: string | null;
};

export default function Catalog() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const { data } = useQuery<CatalogStatus>({
    queryKey: ["catalog"],
    queryFn: () => api("/api/admin/catalog"),
    refetchInterval: 10_000,
  });

  const reload = useMutation({
    mutationFn: () => api("/api/admin/catalog/reload", { method: "POST" }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["catalog"] });
      qc.invalidateQueries({ queryKey: ["apps"] });
    },
  });

  if (!data || !me) {
    return (
      <div className="py-20 text-zinc-500 text-xs font-mono">
        loading catalog...
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white tracking-tight">
          Catalog
        </h1>
        <Button
          size="default"
          onClick={() => reload.mutate()}
          disabled={reload.isPending}
        >
          <RefreshCw className={`size-3.5 ${reload.isPending ? "animate-spin" : ""}`} strokeWidth={1.5} />
          <span>{reload.isPending ? "Reloading..." : "Reload now"}</span>
        </Button>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <Card className="p-5">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider mb-2">Catalog File</div>
          <div className="font-mono text-sm text-zinc-200 truncate" title={data.path}>
            {data.path}
          </div>
        </Card>

        <Card className="p-5">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider mb-2">Active Revision</div>
          <div className="font-mono text-sm text-zinc-100">
            {data.rev ? data.rev.slice(0, 10) : "none"}{" "}
            <span className="text-xs text-zinc-400">({data.apps} apps)</span>
          </div>
        </Card>

        <Card className="p-5">
          <div className="text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider mb-2">Last Sync</div>
          <div className="font-mono text-sm text-zinc-300">
            {formatTime(data.loaded_at, me.display_tz)}
          </div>
        </Card>
      </div>

      {data.last_error && (
        <div className="rounded-xl bg-rose-500/10 p-4 text-xs text-rose-300 font-mono">
          <div className="font-semibold mb-2">Catalog reload rejected:</div>
          <pre className="whitespace-pre-wrap">{data.last_error}</pre>
        </div>
      )}
    </div>
  );
}
