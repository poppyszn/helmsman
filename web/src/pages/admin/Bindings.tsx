import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type App } from "@/lib/api";
import { Table, Td, Th } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

type B = { id: number; subject_type: string; subject_name: string; role_name: string; app_id: string; env: string };
type Role = { id: number; name: string; permissions: string[] };

const field = "h-9 rounded-md border border-border bg-card px-2 text-sm";

export default function Bindings() {
  const qc = useQueryClient();
  const bindings = useQuery<B[]>({ queryKey: ["bindings"], queryFn: () => api("/api/admin/bindings") });
  const roles = useQuery<Role[]>({ queryKey: ["roles"], queryFn: () => api("/api/admin/roles") });
  const apps = useQuery<App[]>({ queryKey: ["apps"], queryFn: () => api("/api/apps") });
  const [form, setForm] = useState({ subject_type: "user", subject: "", role_id: 0, app_id: "*", env: "*" });
  const [err, setErr] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api("/api/admin/bindings", { method: "POST", json: { ...form, role_id: form.role_id || roles.data?.[0]?.id } }),
    onSuccess: () => { setErr(null); setForm({ ...form, subject: "" }); qc.invalidateQueries({ queryKey: ["bindings"] }); },
    onError: (e: Error) => setErr(e.message),
  });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/api/admin/bindings/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["bindings"] }),
  });

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Access</h1>
      <Card>
        <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
          <label className="text-sm">Type<br />
            <select className={field} value={form.subject_type} onChange={(e) => setForm({ ...form, subject_type: e.target.value })}>
              <option value="user">User</option><option value="team">Team</option>
            </select>
          </label>
          <label className="text-sm">{form.subject_type === "user" ? "GitHub login" : "org/team"}<br />
            <input className={field} required value={form.subject} onChange={(e) => setForm({ ...form, subject: e.target.value })} />
          </label>
          <label className="text-sm">Role<br />
            <select className={field} value={form.role_id} onChange={(e) => setForm({ ...form, role_id: Number(e.target.value) })}>
              {roles.data?.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
            </select>
          </label>
          <label className="text-sm">App<br />
            <select className={field} value={form.app_id} onChange={(e) => setForm({ ...form, app_id: e.target.value })}>
              <option value="*">All apps</option>
              {apps.data?.map((a) => <option key={a.id} value={a.id}>{a.id}</option>)}
            </select>
          </label>
          <label className="text-sm">Env<br />
            <input className={`${field} w-28`} value={form.env} onChange={(e) => setForm({ ...form, env: e.target.value })} />
          </label>
          <Button type="submit" disabled={create.isPending}>Grant</Button>
        </form>
        <p className="mt-2 text-xs text-muted">An env of * covers non-production environments only. Name the env (for example prod) to grant production access.</p>
        {err && <p role="alert" className="mt-2 text-sm text-red-600 dark:text-red-400">{err}</p>}
      </Card>
      <Table>
        <thead><tr><Th>Subject</Th><Th>Role</Th><Th>App</Th><Th>Env</Th><Th /></tr></thead>
        <tbody>
          {bindings.data?.map((b) => (
            <tr key={b.id}>
              <Td>{b.subject_name} <span className="text-xs text-muted">({b.subject_type})</span></Td>
              <Td>{b.role_name}</Td><Td>{b.app_id}</Td><Td>{b.env}</Td>
              <Td><Button variant="outline" size="sm" onClick={() => remove.mutate(b.id)}>Remove</Button></Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
