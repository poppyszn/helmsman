import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Anchor } from "lucide-react";
import { api, useMe } from "@/lib/api";
import { Button } from "@/components/ui/button";

const link = ({ isActive }: { isActive: boolean }) =>
  `rounded px-2 py-1 text-sm ${isActive ? "bg-border/60 font-medium" : "text-muted hover:text-foreground"}`;

export default function Layout() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const nav = useNavigate();
  if (!me) return null;
  const signOut = async () => {
    await api("/auth/logout", { method: "POST" });
    qc.clear();
    nav("/login");
  };
  return (
    <div className="min-h-screen">
      <header className="flex flex-wrap items-center gap-4 border-b border-border px-6 py-3">
        <Link to="/" className="flex items-center gap-2 font-semibold">
          <Anchor className="size-5" aria-hidden /> Helmsman
        </Link>
        <nav className="flex flex-wrap gap-1" aria-label="Main">
          <NavLink to="/" end className={link}>Apps</NavLink>
          {me.can.admin && <NavLink to="/admin/users" className={link}>Users</NavLink>}
          {me.can.admin && <NavLink to="/admin/bindings" className={link}>Access</NavLink>}
          {me.can.admin && <NavLink to="/admin/catalog" className={link}>Catalog</NavLink>}
          {me.can.agents && <NavLink to="/admin/agents" className={link}>Agents</NavLink>}
          {me.can.audit && <NavLink to="/admin/audit" className={link}>Audit log</NavLink>}
        </nav>
        <div className="ml-auto flex items-center gap-3 text-sm">
          <span className="text-muted">{me.user.login}</span>
          <Button variant="outline" size="sm" onClick={signOut}>Sign out</Button>
        </div>
      </header>
      <main className="mx-auto max-w-5xl p-6">
        <Outlet />
      </main>
    </div>
  );
}
