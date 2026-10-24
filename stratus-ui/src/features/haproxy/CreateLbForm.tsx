import { useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

import { useOperation } from "@/features/prepare/useOperation";
import { createVmErrors, type CreateVmErrors, type CreateVmInput } from "@/features/prepare/validation";

import { createLb } from "./api";

// busy es verdadero mientras otra operación de la pantalla está en curso: desactiva el botón.
type Props = { busy: boolean; onDone(): void };

// Campos del formulario; los numéricos se guardan como texto y se convierten al enviar.
const FIELDS: { key: keyof CreateVmInput; label: string; type: "text" | "number"; placeholder?: string }[] = [
  { key: "name", label: "Nombre", type: "text", placeholder: "lb1" },
  { key: "memory_mb", label: "Memoria (MB)", type: "number" },
  { key: "cpus", label: "CPUs", type: "number" },
];

const INITIAL: Record<keyof CreateVmInput, string> = { name: "", memory_mb: "2048", cpus: "2" };

// Formulario "Crear balanceador": nombre, memoria y CPUs, validados con las reglas de 001 (REQ-1).
export function CreateLbForm({ busy, onDone }: Props) {
  const [values, setValues] = useState(INITIAL);
  const [errors, setErrors] = useState<CreateVmErrors>({});
  const { pending, result, run } = useOperation();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const input: CreateVmInput = {
      name: values.name,
      memory_mb: Number(values.memory_mb),
      cpus: Number(values.cpus),
    };
    const found = createVmErrors(input);
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    await run(() => createLb(input));
    onDone();
  }

  return (
    // noValidate: la validación es la de validation.ts, con mensajes en español.
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        {FIELDS.map(({ key, label, type, placeholder }) => (
          <Field key={key} data-invalid={errors[key] ? true : undefined}>
            <FieldLabel htmlFor={`create-lb-${key}`}>{label}</FieldLabel>
            <Input
              id={`create-lb-${key}`}
              type={type}
              placeholder={placeholder}
              value={values[key]}
              onChange={(event) => setValues({ ...values, [key]: event.target.value })}
              aria-invalid={errors[key] ? true : undefined}
            />
            {errors[key] && <FieldError>{errors[key]}</FieldError>}
          </Field>
        ))}
        <Field orientation="horizontal">
          <Button type="submit" disabled={pending || busy}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Crear balanceador
          </Button>
        </Field>
        <OperationResult result={result} />
      </FieldGroup>
    </form>
  );
}
