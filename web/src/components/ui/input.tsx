import * as React from "react";
import { cn } from "@/lib/utils";

export type InputProps = React.InputHTMLAttributes<HTMLInputElement>;

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, ...props }, ref) => {
    return (
      <input
        type={type}
        className={cn(
          "flex h-9 w-full rounded-md bg-[#121215] px-3 py-1.5 text-xs text-zinc-100 placeholder:text-zinc-500 transition-colors focus:outline-none focus:ring-1 focus:ring-zinc-400 disabled:cursor-not-allowed disabled:opacity-40 font-mono-code",
          className
        )}
        ref={ref}
        {...props}
      />
    );
  }
);
Input.displayName = "Input";
