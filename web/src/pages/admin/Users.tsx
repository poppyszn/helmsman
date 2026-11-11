import { useQuery } from "@tanstack/react-query";
import { api, formatTime, useMe } from "@/lib/api";
import { Table, Td, Th } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";

type U = { id: number; login: string; name: string | null; is_admin: boolean; disabled_at: string | null; last_login_at: string | null; orgs: string[] };

export default function Users() {
  const { data: me } = useMe();
  const { data, isPending } = useQuery<U[]>({ queryKey: ["admin-users"], queryFn: () => api("/api/admin/users") });
  if (isPending || !me) return <p className="text-sm text-muted">Loading</p>;
  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Users</h1>
      <Table>
        <thead><tr><Th>Login</Th><Th>Name</Th><Th>Orgs</Th><Th>Last sign-in</Th><Th>Flags</Th></tr></thead>
        <tbody>
          {data!.map((u) => (
            <tr key={u.id}>
              <Td className="font-medium">{u.login}</Td>
              <Td>{u.name ?? ""}</Td>
              <Td>{u.orgs.join(", ")}</Td>
              <Td>{formatTime(u.last_login_at, me.display_tz)}</Td>
              <Td>
                {u.is_admin && <Badge tone="ok">admin</Badge>} {u.disabled_at && <Badge tone="bad">disabled</Badge>}
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
