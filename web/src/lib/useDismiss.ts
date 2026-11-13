import * as React from "react";

/** Closes a popover on Escape or a click outside it. One place, so every popover behaves the same. */
export function useDismiss(open: boolean, ref: React.RefObject<HTMLElement | null>, close: () => void) {
  React.useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) close();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      close();
      // Back to the trigger (the first button in the popover's wrapper), so the keyboard does not lose its place.
      ref.current?.querySelector<HTMLElement>("button")?.focus();
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [open, ref, close]);
}

const ITEM = "[data-menu-item]";

/** Arrow keys, Home and End move between the items of a popover list. Mark each item with data-menu-item. */
export function menuKeyDown(e: React.KeyboardEvent<HTMLElement>) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
  const items = [...e.currentTarget.querySelectorAll<HTMLElement>(ITEM)];
  if (items.length === 0) return;
  e.preventDefault();
  const at = items.indexOf(document.activeElement as HTMLElement);
  const next = e.key === "Home" ? 0 : e.key === "End" ? items.length - 1 : e.key === "ArrowDown" ? (at + 1) % items.length : (at <= 0 ? items.length - 1 : at - 1);
  items[next].focus();
}

/** When a popover opens, put the keyboard on its current item (or the first one), unless a field already has it. */
export function useFocusItem(open: boolean, ref: React.RefObject<HTMLElement | null>) {
  React.useEffect(() => {
    if (!open || !ref.current) return;
    if (ref.current.querySelector("input:focus")) return;
    const items = ref.current.querySelectorAll<HTMLElement>(ITEM);
    (ref.current.querySelector<HTMLElement>(`${ITEM}[aria-current], ${ITEM}[aria-pressed="true"]`) ?? items[0])?.focus();
  }, [open, ref]);
}
