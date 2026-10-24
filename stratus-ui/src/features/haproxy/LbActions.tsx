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
import { useOperation } from "@/features/prepare/useOperation";

import { deleteLb, refreshLb } from "./api";
import type { Lb } from "./types";

type Props = { lb: Lb | null; onDone(): void };

type Action = "refresh" | "delete";

// Acciones sobre el balanceador seleccionado: "Actualizar IPs" (REQ-14) y "Eliminar" con confirmación
// que nombra el balanceador y su red interna (REQ-6). El script solo se invoca al confirmar.
export function LbActions({ lb, onDone }: Props) {
  const { pending, result, run } = useOperation();
  const [active, setActive] = useState<Action | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const disabled = lb === null || pending;

  async function execute(action: Action, fn: (name: string) => Promise<string>) {
    if (!lb) {
      return;
    }
    setActive(action);
    await run(() => fn(lb.name));
    setActive(null);
    onDone();
  }

  return (
    <FieldGroup>
      <Field orientation="horizontal">
        <Button variant="outline" disabled={disabled} onClick={() => void execute("refresh", refreshLb)}>
          {active === "refresh" && <Spinner data-icon="inline-start" aria-label="Cargando" />}
          Actualizar IPs
        </Button>
        <Button variant="destructive" disabled={disabled} onClick={() => setConfirmOpen(true)}>
          {active === "delete" && <Spinner data-icon="inline-start" aria-label="Cargando" />}
          Eliminar
        </Button>
      </Field>
      {lb === null && <FieldDescription>Seleccione un balanceador para continuar</FieldDescription>}

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Eliminar el balanceador {lb?.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              Se apagará a la fuerza si está encendido y se eliminará de VirtualBox junto con su disco diferencial,
              su red interna stratus-{lb?.name} y su servidor DHCP. No puede tener servidores asignados. Esta acción
              no se puede deshacer.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmOpen(false);
                void execute("delete", deleteLb);
              }}
            >
              Confirmar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <OperationResult result={result} />
    </FieldGroup>
  );
}
