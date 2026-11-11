import { TriangleAlertIcon } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

type Props = { warning: string };

// Advertencia de una operación que terminó bien pero dejó algo pendiente (004 REQ-14, 005 REQ-5,
// REQ-24, REQ-26). Varias advertencias llegan en el mismo texto, una por línea.
export function WarningAlert({ warning }: Props) {
  if (!warning) {
    return null;
  }
  return (
    <Alert>
      <TriangleAlertIcon />
      <AlertTitle>Advertencia</AlertTitle>
      <AlertDescription className="whitespace-pre-line">{warning}</AlertDescription>
    </Alert>
  );
}
