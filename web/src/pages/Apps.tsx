import * as React from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api, type App } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { StatusBadge } from "@/components/StatusBadge";
import { Input } from "@/components/ui/input";

export default function Apps() {
  const [filter, setFilter] = React.useState("");
  const [tierFilter, setTierFilter] = React.useState<"all" | "prod" | "nonprod">("all");

  const { data = [], isPending, error } = useQuery<App[]>({
    queryKey: ["apps"],
    queryFn: () => api("/api/apps"),
    refetchInterval: 15_000,
  });

  if (isPending) {
    return (
      <div className="py-20 text-xs text-zinc-500 font-mono">
        loading apps...
      </div>
    );
  }

  if (error) {
    return (
      <Card className="bg-[#0a0a0d]">
        <h3 className="text-sm font-semibold text-amber-400">Access Restricted</h3>
        <p className="text-xs text-zinc-400 mt-1">
          You do not have permission to view any applications in the catalog.
        </p>
      </Card>
    );
  }

  const filteredApps = data.filter((app) => {
    const matchesQuery =
      app.name.toLowerCase().includes(filter.toLowerCase()) ||
      app.repo.toLowerCase().includes(filter.toLowerCase()) ||
      app.environments.some((e) => e.env.toLowerCase().includes(filter.toLowerCase()));

    if (!matchesQuery) return false;
    if (tierFilter === "all") return true;
    return app.environments.some((e) => e.tier === tierFilter);
  });

  return (
    <div className="space-y-6">
      {/* Header & Filter Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <h1 className="text-2xl font-bold text-white tracking-tight">
          Applications
        </h1>

        <div className="flex items-center gap-3">
          <div className="relative w-60">
            <Search className="absolute left-3 top-2.5 size-3.5 text-zinc-500" strokeWidth={1.5} />
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Search apps or repos..."
              className="pl-8 h-9 text-xs bg-[#121215]"
            />
          </div>

          <div className="flex rounded-lg bg-[#121215] p-1 text-xs font-mono">
            {(["all", "prod", "nonprod"] as const).map((tier) => (
              <button
                key={tier}
                onClick={() => setTierFilter(tier)}
                className={`rounded-md px-3 py-1 text-xs capitalize transition-colors cursor-pointer select-none font-medium ${
                  tierFilter === tier
                    ? "bg-[#202028] text-white shadow-sm"
                    : "text-zinc-400 hover:text-zinc-200"
                }`}
              >
                {tier}
              </button>
            ))}
          </div>
        </div>
      </div>

      {filteredApps.length === 0 ? (
        <div className="py-16 text-center text-sm text-zinc-500 font-mono">
          {filter ? "no matching apps" : "no apps declared in catalog"}
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          {filteredApps.map((app) => (
            <Link key={app.id} to={`/apps/${app.id}`} className="block focus:outline-none group">
              <Card className="hover:bg-[#111116] transition-all duration-150 p-5">
                <div className="flex items-baseline justify-between gap-3">
                  <h2 className="text-base font-semibold text-white tracking-tight group-hover:text-zinc-100">
                    {app.name}
                  </h2>
                  <span className="text-xs text-zinc-400 font-mono">
                    {app.repo}
                  </span>
                </div>

                <div className="mt-4 flex flex-wrap gap-2.5">
                  {app.environments.map((e) => (
                    <div
                      key={e.env}
                      className="flex items-center gap-2 rounded-lg bg-[#141418] px-2.5 py-1.5 text-xs transition-colors hover:bg-[#1a1a22]"
                    >
                      <span className="font-mono text-xs font-medium text-zinc-200">{e.env}</span>
                      <Badge tone={e.tier === "prod" ? "prod" : "default"}>
                        {e.tier}
                      </Badge>
                      <StatusBadge status={e.status} size="sm" showLabel={false} />
                    </div>
                  ))}
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
