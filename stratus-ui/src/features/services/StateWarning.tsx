import { TriangleAlertIcon } from "lucide-react";

import { Alert, AlertDescription } from "@/components/ui/alert";

type Props = { name: string; state?: string; hint?: string };

// Aviso cuando el estado leído tras crear, editar, redesplegar, iniciar o reiniciar no es
// "active" (por ejemplo "failed" o "activating (auto-restart)"): la operación tuvo éxito, pero
// la aplicación no quedó corriendo (REQ-15). No muestra nada si no hay estado o es "active". hint
// es a dónde mirar: los servicios de 003 tienen Estado y Logs; los de 005, solo Estado.
export function StateWarning({ name, state, hint = "revise Estado y Logs" }: Props) {
  if (!state || state === "active") {
    return null;
  }
  return (
    <Alert>
      <TriangleAlertIcon />
      <AlertDescription>
        El servicio {name} quedó en estado {state}; {hint}
      </AlertDescription>
    </Alert>
  );
}
