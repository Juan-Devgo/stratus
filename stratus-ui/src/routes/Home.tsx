import { ModuleCard } from "@/features/home/ModuleCard";
import { MODULES } from "@/features/home/modules";

export function Home() {
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">Módulos</h1>
        <p className="text-muted-foreground">Consola web para administrar máquinas virtuales Debian 13.</p>
      </div>
      <ul aria-label="Módulos" className="grid gap-4 sm:grid-cols-2">
        {MODULES.map((module) => (
          <li key={module.id}>
            <ModuleCard module={module} />
          </li>
        ))}
      </ul>
    </main>
  );
}
