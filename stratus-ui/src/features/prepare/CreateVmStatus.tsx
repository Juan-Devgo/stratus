import { OperationResult } from "@/components/OperationResult";
import { Spinner } from "@/components/ui/spinner";

import type { OperationOutcome } from "./useOperation";

type Props = { name: string | null; pending: boolean; result: OperationOutcome | null };

// Progreso de la creación de la VM fuera del diálogo, para seguirlo aunque se cierre.
export function CreateVmStatus({ name, pending, result }: Props) {
  if (pending) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner aria-label="Cargando" />
        <span>Creando la VM {name}; puede tardar varios minutos.</span>
      </div>
    );
  }
  return <OperationResult result={result} />;
}
