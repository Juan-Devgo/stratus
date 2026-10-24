import { useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";

import { setIssue } from "./api";
import { composeIssue, ISSUE_FIELDS } from "./issue-preview";
import type { IssueField, Vm } from "./types";
import { useOperation } from "./useOperation";
import { validateIssue } from "./validation";

type Props = { vm: Vm | null; onDone(): void };

// Editor de /etc/issue (REQ-10, REQ-11): campos dinámicos, mensaje propio y vista previa en vivo.
export function IssueForm({ vm, onDone }: Props) {
  const [message, setMessage] = useState("");
  const [checked, setChecked] = useState<IssueField[]>([]);
  const [error, setError] = useState<string | null>(null);
  const { pending, result, run } = useOperation();

  // Se envían en el orden de ISSUE_FIELDS, el mismo de la vista previa (REQ-26).
  const fields = ISSUE_FIELDS.map((field) => field.key).filter((key) => checked.includes(key));
  const preview = composeIssue(message, fields);

  function toggle(key: IssueField, on: boolean) {
    setChecked((current) => (on ? [...current, key] : current.filter((item) => item !== key)));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!vm) {
      return;
    }
    const found = validateIssue({ message, fields });
    setError(found);
    if (found) {
      return;
    }
    await run(() => setIssue(vm.name, { message, fields }));
    onDone();
  }

  return (
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        <FieldSet>
          <FieldLegend variant="label">Campos dinámicos</FieldLegend>
          <FieldDescription>Cada campo marcado se agrega en su propia línea después del mensaje.</FieldDescription>
          <FieldGroup className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {ISSUE_FIELDS.map((field) => (
              <Field key={field.key} orientation="horizontal">
                <Checkbox
                  id={`issue-${field.key}`}
                  checked={checked.includes(field.key)}
                  onCheckedChange={(on) => toggle(field.key, on)}
                />
                <FieldLabel htmlFor={`issue-${field.key}`} className="font-normal">
                  {field.label}
                </FieldLabel>
              </Field>
            ))}
          </FieldGroup>
        </FieldSet>
        <Field data-invalid={error ? true : undefined}>
          <FieldLabel htmlFor="issue-message">Mensaje</FieldLabel>
          <Textarea
            id="issue-message"
            placeholder="Servidor de pruebas del equipo"
            value={message}
            onChange={(event) => setMessage(event.target.value)}
            aria-invalid={error ? true : undefined}
          />
          {error ? (
            <FieldError>{error}</FieldError>
          ) : (
            <FieldDescription>Hasta 500 caracteres; se permiten tildes, ñ y saltos de línea, sin "\".</FieldDescription>
          )}
        </Field>
        <Field>
          <FieldTitle>Vista previa</FieldTitle>
          {/* Los escapes (\n, \d...) se muestran tal cual; agetty los reemplaza en la VM. */}
          <pre
            aria-label="Vista previa de /etc/issue"
            className="min-h-16 rounded-lg border bg-muted px-2.5 py-2 font-mono text-sm whitespace-pre-wrap"
          >
            {preview}
          </pre>
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
