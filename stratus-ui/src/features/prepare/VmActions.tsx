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
import { Field, FieldDescription, FieldGroup } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { WarningAlert } from "@/components/WarningAlert";

import { type ActionResult, deleteVm, startVm, stopVm } from "./api";
import type { Vm } from "./types";
import { useOperation } from "./useOperation";

type Props = { vm: Vm | null; onDone(): void };

type Action = "start" | "stop" | "delete";

// Acciones sobre la VM seleccionada: iniciar, detener y eliminar con confirmación (REQ-3).
export function VmActions({ vm, onDone }: Props) {
  const { pending, result, run } = useOperation();
  // Acción en curso, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<Action | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  // Advertencia de la última operación de encender o eliminar; se borra al empezar cualquier otra.
  const [warning, setWarning] = useState("");
  const disabled = vm === null || pending;

  async function execute(action: Action, fn: (name: string) => Promise<string>) {
    if (!vm) {
      return;
    }
    setActive(action);
    setWarning("");
    await run(() => fn(vm.name));
    setActive(null);
    onDone();
  }

  // Encender y eliminar devuelven además una advertencia opcional: el mensaje va al resultado y la
  // advertencia, aparte.
  function withWarning(fn: (name: string) => Promise<ActionResult>) {
    return async (name: string): Promise<string> => {
      const res = await fn(name);
      setWarning(res.warning ?? "");
      return res.message;
    };
  }

  function label(action: Action, text: string) {
    return (
      <>
        {active === action && <Spinner data-icon="inline-start" aria-label="Cargando" />}
        {text}
      </>
    );
  }

  return (
    <FieldGroup>
      <Field orientation="horizontal">
        <Button variant="outline" disabled={disabled} onClick={() => execute("start", withWarning(startVm))}>
          {label("start", "Iniciar")}
        </Button>
        <Button variant="outline" disabled={disabled} onClick={() => execute("stop", stopVm)}>
          {label("stop", "Detener")}
        </Button>
        <Button variant="destructive" disabled={disabled} onClick={() => setConfirmOpen(true)}>
          {label("delete", "Eliminar")}
        </Button>
      </Field>
      {vm === null && <FieldDescription>Seleccione una VM para continuar</FieldDescription>}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Eliminar la VM {vm?.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              Si está encendida se apagará a la fuerza. Luego se eliminará de VirtualBox junto con su disco
              diferencial. El disco base compartido no se modifica. Esta acción no se puede deshacer.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmOpen(false);
                void execute("delete", withWarning(deleteVm));
              }}
            >
              Confirmar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <OperationResult result={result} />
      <WarningAlert warning={warning} />
    </FieldGroup>
  );
}
