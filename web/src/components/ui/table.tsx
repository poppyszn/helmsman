import * as React from "react";
import { cn } from "@/lib/utils";

export function Table({ className, ...p }: React.TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="overflow-x-auto rounded-xl bg-[#0a0a0d]">
      <table className={cn("w-full text-left text-sm", className)} {...p} />
    </div>
  );
}

export const Th = ({ className, ...p }: React.ThHTMLAttributes<HTMLTableCellElement>) => (
  <th
    className={cn(
      "px-4 py-3 text-xs font-semibold uppercase tracking-wider text-zinc-400 bg-[#060608]",
      className
    )}
    {...p}
  />
);

export const Td = ({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) => (
  <td
    className={cn(
      "px-4 py-3.5 align-middle text-sm text-zinc-200 border-t border-zinc-900/60",
      className
    )}
    {...p}
  />
);

export const Tr = ({ className, ...p }: React.HTMLAttributes<HTMLTableRowElement>) => (
  <tr
    className={cn(
      "hover:bg-[#121216] transition-colors",
      className
    )}
    {...p}
  />
);
