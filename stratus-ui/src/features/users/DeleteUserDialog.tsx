import { useState } from "react";

import { OperationResult } from "@/components/OperationResult";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldLabel } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";

import { deleteUser } from "./api";

type Props = { vm: string; username: string | null; onClose(): void; onDone(): void };

// Confirmación de borrado (REQ-13): nombra al usuario y ofrece borrar también todos sus archivos,
// desmarcado por defecto. El script solo se invoca al confirmar. El resultado se muestra en el
// mismo diálogo, que se cierra con "Cerrar".
export function DeleteUserDialog({ vm, username, onClose, onDone }: Props) {
  return (
    <AlertDialog open={username !== null} onOpenChange={(open) => !open && onClose()}>
      {/* La clave reinicia el estado (casilla y resultado) con cada usuario. */}
      {username !== null && (
        <DeleteUserContent key={username} vm={vm} username={username} onClose={onClose} onDone={onDone} />
      )}
    </AlertDialog>
  );
}

function DeleteUserContent({ vm, username, onClose, onDone }: Props & { username: string }) {
  const [removeFiles, setRemoveFiles] = useState(false);
  const { pending, result, run } = useOperation();

  async function confirm() {
    await run(() => deleteUser(vm, username, removeFiles));
    onDone();
  }

  return (
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>¿Eliminar al usuario {username}?</AlertDialogTitle>
        <AlertDialogDescription>
          Se terminan todos sus procesos y se elimina su cuenta de la VM {vm}. Esta acción no se puede deshacer.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <Field orientation="horizontal">
        <Checkbox
          id="delete-user-files"
          checked={removeFiles}
          disabled={pending || result !== null}
          onCheckedChange={setRemoveFiles}
        />
        <FieldLabel htmlFor="delete-user-files" className="font-normal">
          Eliminar todos sus archivos
        </FieldLabel>
      </Field>
      <OperationResult result={result} />
      <AlertDialogFooter>
        {result === null ? (
          <>
            <AlertDialogCancel disabled={pending}>Cancelar</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={pending} onClick={() => void confirm()}>
              {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
              Confirmar
            </AlertDialogAction>
          </>
        ) : (
          <Button onClick={onClose}>Cerrar</Button>
        )}
      </AlertDialogFooter>
    </AlertDialogContent>
  );
}
