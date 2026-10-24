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
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";

import { deleteService } from "./api";

type Props = { vm: string; name: string | null; onClose(): void; onDone(): void };

// Confirmación de borrado (REQ-25): nombra el servicio y lo que se elimina. El script solo se invoca
// al confirmar. El resultado se muestra en el mismo diálogo, que se cierra con "Cerrar"; si el
// borrado falla el servicio sigue listado y se puede intentar otra vez.
export function DeleteServiceDialog({ vm, name, onClose, onDone }: Props) {
  return (
    <AlertDialog open={name !== null} onOpenChange={(open) => !open && onClose()}>
      {/* La clave reinicia el resultado con cada servicio. */}
      {name !== null && <DeleteContent key={name} vm={vm} name={name} onClose={onClose} onDone={onDone} />}
    </AlertDialog>
  );
}

function DeleteContent({ vm, name, onClose, onDone }: Props & { name: string }) {
  const { pending, result, run } = useOperation();

  async function confirm() {
    await run(() => deleteService(vm, name));
    onDone();
  }

  return (
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>¿Eliminar el servicio {name}?</AlertDialogTitle>
        <AlertDialogDescription>
          Se detiene y deshabilita el servicio en la VM {vm}, y se eliminan su unidad, /etc/{name}.env, /opt/{name} y el
          usuario y el grupo {name}. Esta acción no se puede deshacer.
        </AlertDialogDescription>
      </AlertDialogHeader>
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
