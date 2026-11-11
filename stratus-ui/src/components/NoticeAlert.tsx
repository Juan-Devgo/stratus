import { InfoIcon } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

type Props = { notice: string };

// Aviso de una operación que terminó bien y deja algo que el usuario debe saber, no un fallo
// (005 REQ-41, REQ-42): el registro DNS que quedó quitado o ausente.
export function NoticeAlert({ notice }: Props) {
  if (!notice) {
    return null;
  }
  return (
    <Alert>
      <InfoIcon />
      <AlertTitle>Aviso</AlertTitle>
      <AlertDescription className="whitespace-pre-line">{notice}</AlertDescription>
    </Alert>
  );
}
