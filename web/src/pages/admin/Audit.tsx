import { useQuery } from "@tanstack/react-query";
import { api, formatTime, useMe } from "@/lib/api";
import { Table, Td, Th, Tr } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";

type AuditEntry = {
  id: number;
  at: string;
  actor_type: string;
  actor_id: string;
  action: string;
  target: string | null;
  result: string;
  details: unknown;
};

export default function Audit() {
  const { data: me } = useMe();
  const { data = [], isPending } = useQuery<AuditEntry[]>({
    queryKey: ["audit"],
    queryFn: () => api("/api/admin/audit?limit=200"),
    refetchInterval: 15_000,
  });

  if (isPending || !me) {
    return (
      <div className="py-20 text-zinc-500 text-xs font-mono">
        loading audit trail...
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white tracking-tight">
          Audit Log
        </h1>
      </div>

      <Table>
        <thead>
          <Tr>
            <Th>Timestamp ({me.display_tz})</Th>
            <Th>Actor</Th>
            <Th>Action</Th>
            <Th>Target</Th>
            <Th>Result</Th>
          </Tr>
        </thead>
        <tbody>
          {data.map((a) => (
            <Tr key={a.id}>
              <Td className="whitespace-nowrap font-mono text-xs text-zinc-400">
                {formatTime(a.at, me.display_tz)}
              </Td>
              <Td>
                <span className="font-mono text-sm text-zinc-200 font-medium">{a.actor_id}</span>
                <span className="text-xs text-zinc-500 font-mono ml-2">({a.actor_type})</span>
              </Td>
              <Td className="font-mono text-sm text-zinc-300">
                {a.action}
              </Td>
              <Td className="font-mono text-sm text-zinc-400">
                {a.target || "-"}
              </Td>
              <Td>
                <Badge tone={a.result === "ok" ? "ok" : "bad"}>
                  {a.result}
                </Badge>
              </Td>
            </Tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
