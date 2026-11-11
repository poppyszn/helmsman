import * as React from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Search,
  Layers,
  Users,
  Shield,
  BookOpen,
  Cpu,
  FileText,
  LogOut,
  CornerDownLeft,
} from "lucide-react";
import { api, useMe, type App } from "@/lib/api";

type CommandItem = {
  id: string;
  title: string;
  subtitle?: string;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  section: string;
  onSelect: () => void;
};

export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        onOpenChange(!open);
      } else if (e.key === "Escape" && open) {
        onOpenChange(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [open, onOpenChange]);

  if (!open) return null;

  return <CommandPaletteModal onClose={() => onOpenChange(false)} />;
}

function CommandPaletteModal({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = React.useState("");
  const [selectedIndex, setSelectedIndex] = React.useState(0);
  const inputRef = React.useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const { data: me } = useMe();
  const { data: apps = [] } = useQuery<App[]>({
    queryKey: ["apps"],
    queryFn: () => api("/api/apps"),
  });

  React.useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const items: CommandItem[] = React.useMemo(() => {
    const list: CommandItem[] = [];

    // Apps section
    apps.forEach((app) => {
      list.push({
        id: `app-${app.id}`,
        title: app.name,
        subtitle: app.repo,
        icon: Layers,
        section: "Applications",
        onSelect: () => navigate(`/apps/${app.id}`),
      });
    });

    // Navigation section
    list.push({
      id: "nav-apps",
      title: "Apps",
      icon: Layers,
      section: "Navigation",
      onSelect: () => navigate("/"),
    });

    if (me?.can.admin) {
      list.push({
        id: "nav-users",
        title: "Users",
        icon: Users,
        section: "Navigation",
        onSelect: () => navigate("/admin/users"),
      });
      list.push({
        id: "nav-bindings",
        title: "Access",
        icon: Shield,
        section: "Navigation",
        onSelect: () => navigate("/admin/bindings"),
      });
      list.push({
        id: "nav-catalog",
        title: "Catalog",
        icon: BookOpen,
        section: "Navigation",
        onSelect: () => navigate("/admin/catalog"),
      });
    }

    if (me?.can.agents) {
      list.push({
        id: "nav-agents",
        title: "Agents",
        icon: Cpu,
        section: "Navigation",
        onSelect: () => navigate("/admin/agents"),
      });
    }

    if (me?.can.audit) {
      list.push({
        id: "nav-audit",
        title: "Audit log",
        icon: FileText,
        section: "Navigation",
        onSelect: () => navigate("/admin/audit"),
      });
    }

    // Actions
    list.push({
      id: "act-logout",
      title: "Sign out",
      icon: LogOut,
      section: "Actions",
      onSelect: async () => {
        await api("/auth/logout", { method: "POST" });
        navigate("/login");
      },
    });

    if (!query.trim()) return list;

    const q = query.toLowerCase();
    return list.filter(
      (item) =>
        item.title.toLowerCase().includes(q) ||
        (item.subtitle && item.subtitle.toLowerCase().includes(q))
    );
  }, [apps, me, query, navigate]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev + 1) % (items.length || 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev - 1 + (items.length || 1)) % (items.length || 1));
    } else if (e.key === "Enter" && items[selectedIndex]) {
      e.preventDefault();
      items[selectedIndex].onSelect();
      onClose();
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-start justify-center pt-24 px-4 bg-black/85 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg overflow-hidden rounded-xl bg-[#0c0c10] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 px-4 py-3 border-b border-zinc-900/80">
          <Search className="size-4 text-zinc-400" strokeWidth={1.5} />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            onKeyDown={handleKeyDown}
            placeholder="Type a command or app..."
            className="w-full bg-transparent text-sm text-zinc-100 placeholder:text-zinc-500 focus:outline-none font-mono"
          />
        </div>

        <div className="max-h-72 overflow-y-auto p-2 space-y-1 font-mono">
          {items.length === 0 ? (
            <div className="py-8 text-center text-xs text-zinc-500">
              No results found.
            </div>
          ) : (
            items.map((item, idx) => {
              const Icon = item.icon;
              const isSelected = idx === selectedIndex;
              return (
                <div
                  key={item.id}
                  onClick={() => {
                    item.onSelect();
                    onClose();
                  }}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  className={`flex items-center gap-3 rounded-lg px-3 py-2 text-xs cursor-pointer select-none transition-colors ${
                    isSelected
                      ? "bg-[#1a1a24] text-white"
                      : "text-zinc-400 hover:bg-[#121218] hover:text-zinc-200"
                  }`}
                >
                  <Icon className="size-4 text-zinc-400 shrink-0" strokeWidth={1.5} />
                  <div className="flex-1 min-w-0">
                    <span className="font-medium text-zinc-200">{item.title}</span>
                    {item.subtitle && (
                      <span className="text-xs text-zinc-500 ml-2">
                        {item.subtitle}
                      </span>
                    )}
                  </div>
                  {isSelected && (
                    <CornerDownLeft className="size-3.5 text-zinc-400 shrink-0" strokeWidth={1.5} />
                  )}
                </div>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}
