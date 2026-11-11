import { useQuery } from "@tanstack/react-query";
import { Anchor } from "lucide-react";
import { Card } from "@/components/ui/card";

type Ready = { status: string };

function useReady() {
  return useQuery<Ready>({
    queryKey: ["readyz"],
    queryFn: async () => {
      const res = await fetch("/readyz");
      if (!res.ok) throw new Error(`readyz ${res.status}`);
      return res.json();
    },
    refetchInterval: 10_000,
    retry: false,
  });
}

export default function App() {
  const ready = useReady();
  const label = ready.isPending ? "Checking" : ready.isError ? "Server unavailable" : "Server ready";
  return (
    <div className="min-h-screen">
      <header className="flex items-center gap-2 border-b border-border px-6 py-3">
        <Anchor className="size-5" aria-hidden />
        <span className="font-semibold">Helmsman</span>
      </header>
      <main className="mx-auto max-w-4xl p-6">
        <h1 className="mb-4 text-xl font-semibold">Apps</h1>
        <Card>
          <p className="text-sm text-muted">
            No apps yet. Login and the app catalog arrive in the next milestone.
          </p>
          <p className="mt-3 text-sm" role="status">
            {label}
          </p>
        </Card>
      </main>
    </div>
  );
}
