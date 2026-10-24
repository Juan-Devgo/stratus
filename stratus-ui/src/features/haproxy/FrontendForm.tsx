import { useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";

import { saveFrontend } from "./api";
import type { Lb } from "./types";
import { validateFrontendPort } from "./validation";

type Props = { lb: Lb | null; onDone(): void };

// Puerto del frontend del balanceador seleccionado (REQ-7): por defecto 80 y con el valor actual
// ya escrito. El puerto 22 y los fuera de 1-65535 se rechazan aquí, sin llamar al backend.
export function FrontendForm({ lb, onDone }: Props) {
  // La clave reinicia el campo y el resultado al cambiar de balanceador. No incluye el puerto guardado:
  // al guardar, la pantalla recarga la lista y el resultado de la operación debe seguir visible.
  return <FrontendFields key={lb?.name ?? ""} lb={lb} onDone={onDone} />;
}

function FrontendFields({ lb, onDone }: Props) {
  const [port, setPort] = useState(String(lb?.frontend_port ?? 80));
  const [error, setError] = useState<string | null>(null);
  const { pending, result, run } = useOperation();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!lb) {
      return;
    }
    const found = validateFrontendPort(port);
    setError(found);
    if (found) {
      return;
    }
    await run(() => saveFrontend(lb.name, Number(port)));
    onDone();
  }

  return (
    // noValidate: la validación es la de validation.ts, con mensajes en español.
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        <Field data-invalid={error ? true : undefined}>
          <FieldLabel htmlFor="frontend-port">Puerto del frontend</FieldLabel>
          <Input
            id="frontend-port"
            type="text"
            inputMode="numeric"
            value={port}
            disabled={lb === null}
            onChange={(event) => setPort(event.target.value)}
            aria-invalid={error ? true : undefined}
          />
          {error && <FieldError>{error}</FieldError>}
          <FieldDescription>
            HAProxy escucha en este puerto de todas las direcciones del balanceador. Con 0 servidores responde 503.
          </FieldDescription>
        </Field>
        <Field orientation="horizontal">
          <Button type="submit" disabled={lb === null || pending}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Guardar puerto
          </Button>
        </Field>
        <OperationResult result={result} />
      </FieldGroup>
    </form>
  );
}
