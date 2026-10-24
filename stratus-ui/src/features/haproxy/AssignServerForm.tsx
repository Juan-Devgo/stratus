import { useState, type FormEvent } from "react";

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
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import type { Vm } from "@/features/prepare/types";
import { useOperation } from "@/features/prepare/useOperation";

import { assignServer } from "./api";
import type { Lb } from "./types";
import { validatePort } from "./validation";

type Props = { lb: Lb | null; vms: Vm[]; onDone(): void };

// Asignar una VM como servidor del balanceador seleccionado (REQ-8, REQ-10). Solo ofrece las VMs
// comunes (rol "vm"). Asignar reinicia la VM, así que pide confirmación antes de llamar al backend.
export function AssignServerForm({ lb, vms, onDone }: Props) {
  // La clave reinicia el formulario (VM elegida, puerto y resultado) al cambiar de balanceador.
  return <AssignFields key={lb?.name ?? ""} lb={lb} vms={vms} onDone={onDone} />;
}

function AssignFields({ lb, vms, onDone }: Props) {
  const [vm, setVm] = useState<string | null>(null);
  const [port, setPort] = useState("80");
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const { pending, result, run } = useOperation();

  const candidates = vms.filter((item) => item.role === "vm");
  // Base UI necesita la lista de ítems en la raíz; el primero (value null) es el texto inicial.
  const items = [
    { label: candidates.length > 0 ? "Seleccione una VM" : "No hay VMs disponibles", value: null },
    ...candidates.map((item) => ({ label: item.name, value: item.name })),
  ];

  // El puerto se valida antes de pedir la confirmación; el backend no se llama hasta confirmar.
  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const found = validatePort(port);
    setError(found);
    if (!found && vm !== null) {
      setConfirmOpen(true);
    }
  }

  async function confirm() {
    if (!lb || vm === null) {
      return;
    }
    setConfirmOpen(false);
    await run(() => assignServer(lb.name, vm, Number(port)));
    setVm(null);
    onDone();
  }

  return (
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="assign-vm">VM a asignar</FieldLabel>
          <Select items={items} value={vm} disabled={lb === null} onValueChange={(next) => setVm(next)}>
            <SelectTrigger id="assign-vm" className="min-w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {items.map((item) => (
                  <SelectItem key={item.value ?? ""} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field data-invalid={error ? true : undefined}>
          <FieldLabel htmlFor="assign-port">Puerto del servidor</FieldLabel>
          <Input
            id="assign-port"
            type="text"
            inputMode="numeric"
            value={port}
            disabled={lb === null}
            onChange={(event) => setPort(event.target.value)}
            aria-invalid={error ? true : undefined}
          />
          {error && <FieldError>{error}</FieldError>}
          <FieldDescription>Puerto en el que la VM atiende las peticiones que reparte el balanceador.</FieldDescription>
        </Field>
        <Field orientation="horizontal">
          <Button type="submit" disabled={lb === null || vm === null || pending}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Asignar
          </Button>
        </Field>
        <OperationResult result={result} />
      </FieldGroup>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Asignar {vm} a {lb?.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              La VM {vm} se detiene si está encendida, pasa a la red interna del balanceador con una segunda
              interfaz NAT y se reinicia. Si algo falla, vuelve a su red en puente y a su estado anterior.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirm()}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}
