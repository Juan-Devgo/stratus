import { DownloadIcon, KeyRoundIcon } from "lucide-react";
import { type ReactNode, useState } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldGroup } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

import { downloadUserKey, generateUserKey } from "./api";
import type { Vm } from "./types";
import { sshCommand } from "./ssh-command";
import { useOperation } from "./useOperation";

type Props = { vm: Vm | null; onDone(): void };

type Action = "generate" | "download";

// Nombre del archivo descargado; el mismo que se usa en las instrucciones de uso.
function keyFileName(vm: string): string {
  return `id_ed25519_${vm}`;
}

// saveFile ofrece el blob como descarga y suelta la URL temporal de inmediato, para que la
// llave no quede accesible en el navegador más de lo necesario (P5).
export function saveFile(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

// Genera la llave de root del usuario y ofrece su única descarga (REQ-6 a REQ-9). Si la VM ya
// tiene una llave generada por la app, pide confirmación antes de reemplazarla (REQ-7).
export function UserKeyForm({ vm, onDone }: Props) {
  const { pending, result, run } = useOperation();
  // Acción en curso, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<Action | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  // VM para la que se generó una llave en esta pantalla: solo entonces se ofrece la descarga.
  const [generatedFor, setGeneratedFor] = useState<string | null>(null);
  const disabled = vm === null || pending;
  const showDownload = vm !== null && generatedFor === vm.name;

  async function execute(action: Action, fn: (name: string) => Promise<string>) {
    if (!vm) {
      return;
    }
    setActive(action);
    await run(() => fn(vm.name));
    setActive(null);
  }

  async function generate() {
    await execute("generate", async (name) => {
      const message = await generateUserKey(name);
      setGeneratedFor(name);
      return message;
    });
    onDone();
  }

  function download() {
    void execute("download", async (name) => {
      const blob = await downloadUserKey(name);
      saveFile(blob, keyFileName(name));
      return `Llave descargada como ${keyFileName(name)}. El servidor ya no la conserva.`;
    });
  }

  function label(action: Action, text: string, icon: ReactNode) {
    return (
      <>
        {active === action ? <Spinner data-icon="inline-start" aria-label="Cargando" /> : icon}
        {text}
      </>
    );
  }

  return (
    <FieldGroup>
      <Field orientation="horizontal">
        <Button
          disabled={disabled}
          onClick={() => (vm?.user_key_generated ? setConfirmOpen(true) : void generate())}
        >
          {label("generate", "Generar llave", <KeyRoundIcon data-icon="inline-start" />)}
        </Button>
        {showDownload && (
          <Button variant="outline" disabled={disabled} onClick={download}>
            {label("download", "Descargar llave privada", <DownloadIcon data-icon="inline-start" />)}
          </Button>
        )}
      </Field>
      {vm === null && <FieldDescription>Seleccione una VM para continuar</FieldDescription>}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Generar una nueva llave para {vm?.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              La llave anterior dejará de funcionar: quien la use ya no podrá entrar como root a la VM.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmOpen(false);
                void generate();
              }}
            >
              Confirmar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <OperationResult result={result} />

      {showDownload && (
        <Alert>
          <KeyRoundIcon />
          <AlertTitle>Uso correcto de la llave privada</AlertTitle>
          <AlertDescription>
            <ul className="list-disc pl-4">
              <li>Descárguela ahora: el servidor la entrega una sola vez y la descarta a los 10 minutos.</li>
              <li>
                Guárdela en un lugar privado y ajuste sus permisos: <code>chmod 600 {keyFileName(vm.name)}</code>
              </li>
              <li>No la comparta ni la suba a un repositorio.</li>
              <li>
                Conéctese con: <code>{sshCommand(vm, keyFileName(vm.name), "root")}</code>
              </li>
            </ul>
          </AlertDescription>
        </Alert>
      )}
    </FieldGroup>
  );
}
