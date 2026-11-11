import { useQuery } from "@tanstack/react-query";
import { api, formatTime, useMe } from "@/lib/api";
import { Table, Td, Th } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";

type A = { id: number; at: string; actor_type: string; actor_id: string; action: string; target: string | null; result: string; details: unknown };

export default function Audit() {
  const { data: me } = useMe();
  const { data, isPending } = useQuery<A[]>({ queryKey: ["audit"], queryFn: () => api("/api/admin/audit?limit=200"), refetchInterval: 15000 });
  if (isPending || !me) return <p className="text-sm text-muted">Loading</p>;
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Audit log</h1>
      <Table>
        <thead><tr><Th>Time ({me.display_tz})</Th><Th>Actor</Th><Th>Action</Th><Th>Target</Th><Th>Result</Th></tr></thead>
        <tbody>
          {data!.map((a) => (
            <tr key={a.id}>
              <Td className="whitespace-nowrap">{formatTime(a.at, me.display_tz)}</Td>
              <Td>{a.actor_id} <span className="text-xs text-muted">({a.actor_type})</span></Td>
              <Td className="font-mono text-xs">{a.action}</Td>
              <Td>{a.target ?? ""}</Td>
              <Td><Badge tone={a.result === "ok" ? "ok" : "bad"}>{a.result}</Badge></Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
