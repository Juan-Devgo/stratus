import { useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

import { setHostname } from "./api";
import type { Vm } from "./types";
import { useOperation } from "./useOperation";
import { validateHostname } from "./validation";

type Props = { vm: Vm | null; onDone(): void };

// Cambiar el hostname de la VM seleccionada (REQ-12); el script también ajusta /etc/hosts.
export function HostnameForm({ vm, onDone }: Props) {
  const [hostname, setHostnameValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const { pending, result, run } = useOperation();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!vm) {
      return;
    }
    const found = validateHostname(hostname);
    setError(found);
    if (found) {
      return;
    }
    await run(() => setHostname(vm.name, hostname));
    onDone();
  }

  return (
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        <Field data-invalid={error ? true : undefined}>
          <FieldLabel htmlFor="hostname">Hostname</FieldLabel>
          <Input
            id="hostname"
            placeholder="web-01"
            value={hostname}
            onChange={(event) => setHostnameValue(event.target.value)}
            aria-invalid={error ? true : undefined}
          />
          {error ? (
            <FieldError>{error}</FieldError>
          ) : (
            <FieldDescription>También se actualiza la línea 127.0.1.1 de /etc/hosts.</FieldDescription>
          )}
        </Field>
        <Field orientation="horizontal">
          <Button type="submit" disabled={vm === null || pending}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Aplicar
          </Button>
        </Field>
        {vm === null && <FieldDescription>Seleccione una VM para continuar</FieldDescription>}
        <OperationResult result={result} />
      </FieldGroup>
    </form>
  );
}
