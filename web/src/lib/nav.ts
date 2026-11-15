import type { QueryClient } from "@tanstack/react-query";
import type { NavigateFunction } from "react-router-dom";
import {
  Bell,
  BookOpen,
  Cpu,
  FileText,
  GitBranch,
  Layers,
  PlugZap,
  Shield,
  ShieldCheck,
  Stethoscope,
  Users,
  type LucideIcon,
} from "lucide-react";
import { api, type Me } from "@/lib/api";

export type NavItem = {
  id: string;
  to: string;
  title: string;
  icon: LucideIcon;
  permission?: keyof Me["can"];
  end?: boolean;
};

/** Single source of truth for top-level destinations across the sidebar rail and command palette. */
export const NAV_ITEMS: NavItem[] = [
  { id: "nav-apps", to: "/", title: "Apps", icon: Layers, end: true },
  { id: "nav-users", to: "/admin/users", title: "Users", icon: Users, permission: "admin" },
  { id: "nav-bindings", to: "/admin/bindings", title: "Access", icon: Shield, permission: "admin" },
  { id: "nav-catalog", to: "/admin/catalog", title: "Catalog", icon: BookOpen, permission: "admin" },
  { id: "nav-approvals", to: "/approvals", title: "Approvals", icon: ShieldCheck, permission: "approve" },
  { id: "nav-github", to: "/admin/github", title: "GitHub", icon: GitBranch, permission: "admin" },
  { id: "nav-integrations", to: "/admin/integrations", title: "Integrations", icon: PlugZap, permission: "admin" },
  { id: "nav-notifications", to: "/admin/notifications", title: "Notifications", icon: Bell, permission: "admin" },
  { id: "nav-doctor", to: "/admin/doctor", title: "Setup check", icon: Stethoscope, permission: "admin" },
  { id: "nav-agents", to: "/admin/agents", title: "Agents", icon: Cpu, permission: "agents" },
  { id: "nav-audit", to: "/admin/audit", title: "Audit log", icon: FileText, permission: "audit" },
];

/** Navigation destinations the current principal is permitted to reach. */
export function visibleNavItems(me?: Pick<Me, "can"> | null): NavItem[] {
  return NAV_ITEMS.filter((item) => !item.permission || !!me?.can[item.permission]);
}

/** Ends the session on the server, drops cached API state, and navigates to sign-in. */
export async function signOut(qc: QueryClient, navigate: NavigateFunction): Promise<void> {
  await api("/auth/logout", { method: "POST" });
  qc.clear();
  navigate("/login");
}
