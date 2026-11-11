import * as React from "react";
import { cn } from "@/lib/utils";

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-xl bg-[#0a0a0d] p-5 text-zinc-100 transition-colors",
        className
      )}
      {...props}
    />
  );
}
