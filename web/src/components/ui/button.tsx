import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 rounded-md font-medium transition-all duration-100 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-zinc-400 disabled:pointer-events-none disabled:opacity-40 active:scale-[0.98] select-none cursor-pointer",
  {
    variants: {
      variant: {
        default:
          "bg-white text-black font-semibold hover:bg-zinc-200 shadow-sm",
        outline:
          "bg-[#141418] text-zinc-200 hover:bg-[#1c1c22] hover:text-white",
        secondary:
          "bg-[#1a1a20] text-zinc-200 hover:bg-[#24242c] hover:text-white",
        ghost:
          "text-zinc-400 hover:bg-zinc-900/60 hover:text-zinc-100",
        destructive:
          "bg-rose-500/15 text-rose-300 hover:bg-rose-500/25 font-semibold",
      },
      size: {
        default: "h-9 px-4 text-xs",
        sm: "h-8 px-3 text-xs",
        xs: "h-7 px-2.5 text-xs",
        lg: "h-10 px-5 text-sm",
        icon: "size-8 p-0",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export function Button({ className, variant, size, asChild, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
