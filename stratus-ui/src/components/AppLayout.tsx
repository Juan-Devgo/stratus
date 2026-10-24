import { Link, Outlet } from "react-router";

import { Separator } from "@/components/ui/separator";
import { Toaster } from "@/components/ui/sonner";

// Marco común de todas las pantallas: encabezado con enlace al inicio, la ruta hija y los avisos.
export function AppLayout() {
  return (
    <div className="flex min-h-svh flex-col">
      <header className="mx-auto flex w-full max-w-5xl items-center p-4">
        <Link to="/" className="text-lg font-semibold">
          Stratus
        </Link>
      </header>
      <Separator />
      <Outlet />
      {/* Avisos temporales de las pantallas (por ejemplo, la lista de usuarios actualizada). */}
      <Toaster />
    </div>
  );
}
