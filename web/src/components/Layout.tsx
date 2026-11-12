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
  PlugZap,
  ShieldCheck,
  FileText,
  Bell,
  Stethoscope,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
} from "lucide-react";
import { api, useMe } from "@/lib/api";
import { CommandPalette } from "@/components/CommandPalette";

type Crumb = { label: string; to?: string };

/** The path as clickable steps: every step that has a page of its own links to it, the last one is the page itself. */
function breadcrumbs(pathname: string): Crumb[] {
  const parts = pathname.split("/").filter(Boolean);
  const home: Crumb = { label: "helmsman", to: "/" };
  if (parts.length === 0) return [home, { label: "apps" }];
  if (parts[0] === "apps" && parts.length >= 2) return [home, { label: "apps", to: "/" }, { label: parts[1] }];
  if (parts[0] === "admin" && parts.length >= 2) return [home, { label: "admin" }, { label: parts.slice(1).join(" / ") }];
  return [home, ...parts.map((p, i) => ({ label: p, ...(i < parts.length - 1 ? { to: "/" + parts.slice(0, i + 1).join("/") } : {}) }))];
}

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

  const crumbs = breadcrumbs(location.pathname);

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
          {me.can.admin && (
            <NavLink to="/admin/integrations" className={navItemClass} title="Integrations">
              <PlugZap className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Integrations</span>}
            </NavLink>
          )}
          {me.can.admin && (
            <NavLink to="/admin/notifications" className={navItemClass} title="Notifications">
              <Bell className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Notifications</span>}
            </NavLink>
          )}
          {me.can.admin && (
            <NavLink to="/admin/doctor" className={navItemClass} title="Setup check">
              <Stethoscope className="size-4 shrink-0 text-zinc-400 group-hover:text-zinc-200" strokeWidth={1.5} />
              {!collapsed && <span>Setup check</span>}
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
            <nav aria-label="Breadcrumb">
              <ol className="flex items-center gap-2">
                {crumbs.map((c, i) => (
                  <li key={`${c.label}-${i}`} className="flex items-center gap-2">
                    {i > 0 && (
                      <span className="text-zinc-700" aria-hidden>
                        /
                      </span>
                    )}
                    {c.to ? (
                      <Link to={c.to} className="text-zinc-400 underline-offset-4 transition-colors hover:text-white hover:underline">
                        {c.label}
                      </Link>
                    ) : (
                      <span className={i === crumbs.length - 1 ? "text-zinc-200 font-medium" : "text-zinc-500"} aria-current={i === crumbs.length - 1 ? "page" : undefined}>
                        {c.label}
                      </span>
                    )}
                  </li>
                ))}
              </ol>
            </nav>
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
