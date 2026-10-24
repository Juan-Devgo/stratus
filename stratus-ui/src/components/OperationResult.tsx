import { CircleAlertIcon, CircleCheckIcon } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

type Props = { result: { ok: boolean; text: string } | null };

// Mensaje final de cada operación (P6): éxito o fallo con el texto del backend.
export function OperationResult({ result }: Props) {
  if (!result) {
    return null;
  }
  return (
    <Alert variant={result.ok ? "default" : "destructive"}>
      {result.ok ? <CircleCheckIcon /> : <CircleAlertIcon />}
      <AlertTitle>{result.ok ? "Operación completada" : "La operación falló"}</AlertTitle>
      <AlertDescription>{result.text}</AlertDescription>
    </Alert>
  );
}
