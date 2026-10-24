import { DownloadIcon, KeyRoundIcon } from "lucide-react";

import { OperationResult } from "@/components/OperationResult";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { saveFile } from "@/features/prepare/UserKeyForm";
import type { Vm } from "@/features/prepare/types";
import { sshCommand } from "@/features/prepare/ssh-command";
import { useOperation } from "@/features/prepare/useOperation";

import { downloadUserKey } from "./api";

type Props = { vm: Vm; username: string };

// Descarga única de la llave privada de un usuario recién creado y mensaje de uso (REQ-10, REQ-11).
// Si el backend ya la descartó (descargada, vencida o usuario eliminado) responde 410 y se muestra
// su mensaje.
export function UserKeyDownload({ vm, username }: Props) {
  const { pending, result, run } = useOperation();
  // Nombre del archivo descargado; el mismo que se usa en las instrucciones de uso.
  const fileName = `id_ed25519_${vm.name}_${username}`;

  function download() {
    void run(async () => {
      saveFile(await downloadUserKey(vm.name, username), fileName);
      return `Llave descargada como ${fileName}. El servidor ya no la conserva.`;
    });
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <Button variant="outline" disabled={pending} onClick={download}>
          {pending ? <Spinner data-icon="inline-start" aria-label="Cargando" /> : <DownloadIcon data-icon="inline-start" />}
          Descargar llave privada
        </Button>
      </div>
      <OperationResult result={result} />
      <Alert>
        <KeyRoundIcon />
        <AlertTitle>Uso correcto de la llave privada</AlertTitle>
        <AlertDescription>
          <ul className="list-disc pl-4">
            <li>Descárguela ahora: el servidor la entrega una sola vez y la descarta a los 10 minutos.</li>
            <li>
              Guárdela en un lugar privado y ajuste sus permisos: <code>chmod 600 {fileName}</code>
            </li>
            <li>No la comparta ni la suba a un repositorio.</li>
            <li>
              Conéctese con:{" "}
              <code>{sshCommand(vm, fileName, username)}</code>
            </li>
          </ul>
        </AlertDescription>
      </Alert>
    </div>
  );
}
