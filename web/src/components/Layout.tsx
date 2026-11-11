import * as React from "react";
import { Link, NavLink, Outlet, useNavigate, useLocation } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Anchor,
  Layers,
  Users,
  Shield,
  BookOpen,
  Cpu,
  GitBranch,
  ShieldCheck,
  FileText,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
} from "lucide-react";
import { api, useMe } from "@/lib/api";
import { CommandPalette } from "@/components/CommandPalette";

export default function Layout() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const nav = useNavigate();
  const location = useLocation();
  const [collapsed, setCollapsed] = React.useState(false);
  const [paletteOpen, setPaletteOpen] = React.useState(false);

  const approvals = useQuery<{ runs: unknown[] }>({
    queryKey: ["approvals"],
    queryFn: () => api("/api/approvals"),
    enabled: !!me?.can.approve,
    refetchInterval: 15_000,
  });
  const pending = approvals.data?.runs.length ?? 0;

  if (!me) return null;

  const signOut = async () => {
    await api("/auth/logout", { method: "POST" });
    qc.clear();
    nav("/login");
  };

  const navItemClass = ({ isActive }: { isActive: boolean }) =>
    `group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
      isActive
        ? "bg-[#141418] text-white"
        : "text-zinc-400 hover:text-zinc-100 hover:bg-[#101014]"
    }`;

  const pathParts = location.pathname.split("/").filter(Boolean);
  const breadcrumb = pathParts.length === 0 ? "apps" : pathParts.join(" / ");

  return (
    <div className="flex min-h-screen bg-black text-zinc-100 font-sans antialiased">
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />

      {/* Left Navigation Rail */}
      <aside
        className={`sticky top-0 h-screen flex flex-col bg-black transition-all duration-150 z-30 select-none p-3.5 ${
          collapsed ? "w-16" : "w-56"
        }`}
      >
        {/* Brand Header */}
        <div className="flex h-11 items-center justify-between px-2 mb-4">
          <Link to="/" className="flex items-center gap-2.5 font-semibold text-white tracking-tight">
            <Anchor className="size-5 text-white" strokeWidth={2} />
            {!collapsed && (
              <span className="text-sm font-bold tracking-tight">
                Helmsman
              </span>
            )}
          </Link>
          <button
            onClick={() => setCollapsed(!collapsed)}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            className="rounded-md p-1.5 text-zinc-500 hover:text-zinc-300 hover:bg-[#121215] transition-colors"
          >
            {collapsed ? <PanelLeftOpen className="size-4" strokeWidth={1.5} /> : <PanelLeftClose className="size-4" strokeWidth={1.5} />}
          </button>
        </div>

        {/* Navigation Links */}
        <div className="flex-1 overflow-y-auto space-y-1">
          <NavLink to="/" end className={navItemClass} title="Apps">
            <Layers className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
            {!collapsed && <span>Apps</span>}
          </NavLink>

          {me.can.admin && (
            <NavLink to="/admin/users" className={navItemClass} title="Users">
              <Users className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Users</span>}
            </NavLink>
          )}
          {me.can.admin && (
            <NavLink to="/admin/bindings" className={navItemClass} title="Access">
              <Shield className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Access</span>}
            </NavLink>
          )}
          {me.can.admin && (
            <NavLink to="/admin/catalog" className={navItemClass} title="Catalog">
              <BookOpen className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Catalog</span>}
            </NavLink>
          )}
          {me.can.approve && (
            <NavLink to="/approvals" className={navItemClass} title="Approvals">
              <ShieldCheck className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Approvals</span>}
              {!collapsed && pending > 0 && <span className="ml-auto rounded bg-amber-500/15 px-1.5 text-xs font-mono text-amber-300">{pending}</span>}
            </NavLink>
          )}
          {me.can.admin && (
            <NavLink to="/admin/github" className={navItemClass} title="GitHub">
              <GitBranch className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>GitHub</span>}
            </NavLink>
          )}
          {me.can.agents && (
            <NavLink to="/admin/agents" className={navItemClass} title="Agents">
              <Cpu className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Agents</span>}
            </NavLink>
          )}
          {me.can.audit && (
            <NavLink to="/admin/audit" className={navItemClass} title="Audit log">
              <FileText className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Audit log</span>}
            </NavLink>
          )}
        </div>

        {/* Compact User Footer */}
        <div className="pt-3 px-2 flex items-center justify-between text-xs font-mono text-zinc-500 border-t border-zinc-900/60">
          {!collapsed && (
            <div className="flex items-center gap-2 truncate">
              <span className="text-zinc-200 font-medium truncate">{me.user.login}</span>
            </div>
          )}
          <button
            onClick={signOut}
            title="Sign out"
            className="rounded-md p-1.5 text-zinc-400 hover:text-white hover:bg-[#121215] transition-colors"
          >
            <LogOut className="size-4" strokeWidth={1.5} />
          </button>
        </div>
      </aside>

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col min-w-0 px-8 py-4">
        {/* Top Header Bar */}
        <header className="flex h-11 items-center justify-between text-xs mb-6">
          <div className="flex items-center gap-2 font-mono text-sm text-zinc-500">
            <span>helmsman</span>
            <span className="text-zinc-700">/</span>
            <span className="text-zinc-200 font-medium">{breadcrumb}</span>
          </div>

          <button
            onClick={() => setPaletteOpen(true)}
            className="flex items-center gap-2.5 rounded-lg bg-[#101014] hover:bg-[#16161c] px-3 py-1.5 text-xs font-mono text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer select-none"
            title="Command palette (⌘K)"
          >
            <Search className="size-3.5 text-zinc-500" strokeWidth={1.5} />
            <span>Search...</span>
            <kbd className="rounded bg-[#1a1a20] px-1.5 py-0.5 text-xs font-mono text-zinc-300">
              ⌘K
            </kbd>
          </button>
        </header>

        {/* Page Content */}
        <main className="flex-1 max-w-6xl w-full">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
