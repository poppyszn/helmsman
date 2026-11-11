import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, type App } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, Td, Th } from "@/components/ui/table";

export default function AppDetail() {
  const { appId } = useParams();
  const { data, isPending, error } = useQuery<App>({ queryKey: ["app", appId], queryFn: () => api(`/api/apps/${appId}`) });
  if (isPending) return <p className="text-sm text-muted">Loading</p>;
  if (error) {
    return (
      <Card>
        <p className="text-sm">App not found, or you do not have access to it.</p>
        <Link to="/" className="text-sm text-primary underline">Back to apps</Link>
      </Card>
    );
  }
  return (
    <div className="space-y-4">
      <div>
        <Link to="/" className="text-sm text-muted hover:text-foreground">Apps</Link>
        <h1 className="text-xl font-semibold">{data.name}</h1>
        <p className="text-sm text-muted">{data.repo}</p>
      </div>
      <Table>
        <thead>
          <tr><Th>Environment</Th><Th>Tier</Th><Th>URL</Th><Th>Seeds</Th></tr>
        </thead>
        <tbody>
          {data.environments.map((e) => (
            <tr key={e.env}>
              <Td className="font-medium">{e.env}</Td>
              <Td><Badge tone={e.tier === "prod" ? "prod" : "default"}>{e.tier}</Badge></Td>
              <Td><a className="text-primary underline" href={e.url} target="_blank" rel="noreferrer">{e.url}</a></Td>
              <Td>{e.allow_seed ? "allowed" : "blocked"}</Td>
            </tr>
          ))}
        </tbody>
      </Table>
      <p className="text-sm text-muted">Status, logs and actions arrive in a later milestone.</p>
    </div>
  );
}
