import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { api, type App } from "@/lib/api";
import { Table, Td, Th, Tr } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { AccessRequests } from "@/pages/admin/AccessRequests";

type Binding = {
  id: number;
  subject_type: string;
  subject_name: string;
  role_name: string;
  app_id: string;
  env: string;
};
type Role = { id: number; name: string; permissions: string[] };

const selectField =
  "h-9 rounded-md bg-[#121215] px-3 text-xs text-zinc-100 font-mono focus:outline-none cursor-pointer";

export default function Bindings() {
  const qc = useQueryClient();
  const bindings = useQuery<Binding[]>({
    queryKey: ["bindings"],
    queryFn: () => api("/api/admin/bindings"),
  });
  const roles = useQuery<Role[]>({
    queryKey: ["roles"],
    queryFn: () => api("/api/admin/roles"),
  });
  const apps = useQuery<App[]>({
    queryKey: ["apps"],
    queryFn: () => api("/api/apps"),
  });

  const [form, setForm] = useState({
    subject_type: "user",
    subject: "",
    role_id: 0,
    app_id: "*",
    env: "*",
  });
  const [err, setErr] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () =>
      api("/api/admin/bindings", {
        method: "POST",
        json: {
          ...form,
          role_id: form.role_id || roles.data?.[0]?.id,
        },
      }),
    onSuccess: () => {
      setErr(null);
      setForm({ ...form, subject: "" });
      qc.invalidateQueries({ queryKey: ["bindings"] });
    },
    onError: (e: Error) => setErr(e.message),
  });

  const remove = useMutation({
    mutationFn: (id: number) =>
      api(`/api/admin/bindings/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["bindings"] }),
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white tracking-tight">
          Access & Roles
        </h1>
      </div>

      <AccessRequests />

      <Card className="p-5">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <label className="text-xs font-mono font-medium text-zinc-400">
            Type<br />
            <select
              className={selectField}
              value={form.subject_type}
              onChange={(e) => setForm({ ...form, subject_type: e.target.value })}
            >
              <option value="user" className="bg-black text-white">User</option>
              <option value="team" className="bg-black text-white">Team</option>
            </select>
          </label>

          <label className="text-xs font-mono font-medium text-zinc-400">
            {form.subject_type === "user" ? "GitHub Login" : "org/team"}<br />
            <Input
              required
              value={form.subject}
              onChange={(e) => setForm({ ...form, subject: e.target.value })}
              placeholder={form.subject_type === "user" ? "octocat" : "acme-corp/devs"}
              className="w-48 mt-1 font-mono text-xs h-9"
            />
          </label>

          <label className="text-xs font-mono font-medium text-zinc-400">
            Role<br />
            <select
              className={selectField}
              value={form.role_id}
              onChange={(e) => setForm({ ...form, role_id: Number(e.target.value) })}
            >
              {roles.data?.map((r) => (
                <option key={r.id} value={r.id} className="bg-black text-white">
                  {r.name}
                </option>
              ))}
            </select>
          </label>

          <label className="text-xs font-mono font-medium text-zinc-400">
            App<br />
            <select
              className={selectField}
              value={form.app_id}
              onChange={(e) => setForm({ ...form, app_id: e.target.value })}
            >
              <option value="*" className="bg-black text-white">* (all apps)</option>
              {apps.data?.map((a) => (
                <option key={a.id} value={a.id} className="bg-black text-white">
                  {a.name}
                </option>
              ))}
            </select>
          </label>

          <label className="text-xs font-mono font-medium text-zinc-400">
            Environment<br />
            <select
              className={selectField}
              value={form.env}
              onChange={(e) => setForm({ ...form, env: e.target.value })}
            >
              <option value="*" className="bg-black text-white">* (all non-prod)</option>
              <option value="**" className="bg-black text-white">** (all, including prod)</option>
              <option value="staging" className="bg-black text-white">staging</option>
              <option value="prod" className="bg-black text-white">prod</option>
            </select>
          </label>

          <Button type="submit" size="default" disabled={create.isPending}>
            {create.isPending ? "Adding..." : "Add binding"}
          </Button>
        </form>
        {err && (
          <p role="alert" className="mt-2.5 text-xs text-rose-400 font-mono">
            {err}
          </p>
        )}
      </Card>

      <Table>
        <thead>
          <Tr>
            <Th>Subject</Th>
            <Th>Role</Th>
            <Th>App</Th>
            <Th>Environment</Th>
            <Th className="text-right">Actions</Th>
          </Tr>
        </thead>
        <tbody>
          {bindings.data?.map((b) => (
            <Tr key={b.id}>
              <Td>
                <div className="font-mono text-sm font-semibold text-white">
                  {b.subject_name}
                </div>
                <div className="text-xs text-zinc-500 font-mono">{b.subject_type}</div>
              </Td>
              <Td>
                <Badge tone="indigo">{b.role_name}</Badge>
              </Td>
              <Td className="font-mono text-xs text-zinc-300">
                {b.app_id}
              </Td>
              <Td className="font-mono text-xs text-zinc-300">
                <Badge tone={b.env === "prod" ? "prod" : "default"}>
                  {b.env}
                </Badge>
              </Td>
              <Td className="text-right">
                <button
                  onClick={() => remove.mutate(b.id)}
                  disabled={remove.isPending}
                  className="rounded-md p-1.5 text-zinc-500 hover:text-rose-400 hover:bg-rose-500/10 transition-colors"
                  title="Remove binding"
                >
                  <Trash2 className="size-4" strokeWidth={1.5} />
                </button>
              </Td>
            </Tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
