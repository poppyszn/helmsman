import { useQuery } from "@tanstack/react-query";
import { api, formatTime, useMe } from "@/lib/api";
import { Table, Td, Th, Tr } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";

type User = {
  id: number;
  login: string;
  name: string | null;
  is_admin: boolean;
  disabled_at: string | null;
  last_login_at: string | null;
  orgs: string[];
};

export default function Users() {
  const { data: me } = useMe();
  const { data = [], isPending } = useQuery<User[]>({
    queryKey: ["admin-users"],
    queryFn: () => api("/api/admin/users"),
  });

  if (isPending || !me) {
    return (
      <div className="py-20 text-zinc-500 text-xs font-mono">
        loading users...
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white tracking-tight">
          Users
        </h1>
      </div>

      <Table>
        <thead>
          <Tr>
            <Th>GitHub Login</Th>
            <Th>Name</Th>
            <Th>Organizations</Th>
            <Th>Last Sign-in</Th>
            <Th>Flags</Th>
          </Tr>
        </thead>
        <tbody>
          {data.map((u) => (
            <Tr key={u.id}>
              <Td>
                <span className="font-mono text-sm text-zinc-100 font-semibold">{u.login}</span>
              </Td>
              <Td className="text-sm text-zinc-300">{u.name || "-"}</Td>
              <Td className="font-mono text-xs text-zinc-300">
                {u.orgs.join(", ") || "-"}
              </Td>
              <Td className="font-mono text-xs text-zinc-400">
                {formatTime(u.last_login_at, me.display_tz)}
              </Td>
              <Td>
                <div className="flex items-center gap-1.5">
                  {u.is_admin && <Badge tone="ok">admin</Badge>}
                  {u.disabled_at && <Badge tone="bad">disabled</Badge>}
                  {!u.is_admin && !u.disabled_at && <Badge tone="default">active</Badge>}
                </div>
              </Td>
            </Tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
