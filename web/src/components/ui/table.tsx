import * as React from "react";
import { cn } from "@/lib/utils";

export function Table({ className, ...p }: React.TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="overflow-x-auto rounded-lg border border-border bg-card">
      <table className={cn("w-full text-left text-sm", className)} {...p} />
    </div>
  );
}
export const Th = (p: React.ThHTMLAttributes<HTMLTableCellElement>) => (
  <th className="border-b border-border px-3 py-2 font-medium text-muted" {...p} />
);
export const Td = (p: React.TdHTMLAttributes<HTMLTableCellElement>) => (
  <td className="border-b border-border/60 px-3 py-2 align-top" {...p} />
);
