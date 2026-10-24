import { PlusIcon, Trash2Icon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

import type { EnvVar } from "./types";
import type { ServiceFormErrors } from "./validation";

// Fila de variable; locked deja la clave de solo lectura (las claves que la edición muestra ya
// existentes: el usuario escribe de nuevo su valor, o quita la fila).
export type EnvRow = EnvVar & { locked?: boolean };

type Props = {
  rows: EnvRow[];
  onChange(rows: EnvRow[]): void;
  // Errores del formulario: env_<i> por fila y env para el conjunto (ver validation.ts).
  errors: ServiceFormErrors;
  disabled?: boolean;
};

// Filas clave/valor de las variables de entorno (REQ-9). El valor es un secreto (REQ-13): el campo
// está enmascarado y no se autocompleta. Se pueden agregar y quitar filas.
export function EnvVarFields({ rows, onChange, errors, disabled }: Props) {
  function update(index: number, change: Partial<EnvVar>) {
    onChange(rows.map((row, i) => (i === index ? { ...row, ...change } : row)));
  }

  return (
    <div className="flex flex-col gap-3">
      {rows.map((row, index) => (
        <Field key={index} data-invalid={errors[`env_${index}`] ? true : undefined}>
          <div className="flex items-center gap-2">
            <Input
              aria-label={`Clave de la variable ${index + 1}`}
              placeholder="CLAVE"
              autoComplete="off"
              value={row.key}
              readOnly={row.locked}
              disabled={disabled}
              onChange={(event) => update(index, { key: event.target.value })}
              aria-invalid={errors[`env_${index}`] ? true : undefined}
            />
            <Input
              type="password"
              aria-label={`Valor de la variable ${index + 1}`}
              placeholder="valor"
              autoComplete="new-password"
              value={row.value}
              disabled={disabled}
              onChange={(event) => update(index, { value: event.target.value })}
              aria-invalid={errors[`env_${index}`] ? true : undefined}
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={`Quitar variable ${index + 1}`}
              disabled={disabled}
              onClick={() => onChange(rows.filter((_, i) => i !== index))}
            >
              <Trash2Icon />
            </Button>
          </div>
          {errors[`env_${index}`] && <FieldError>{errors[`env_${index}`]}</FieldError>}
        </Field>
      ))}
      {errors.env && <FieldError>{errors.env}</FieldError>}
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() => onChange([...rows, { key: "", value: "" }])}
        >
          <PlusIcon data-icon="inline-start" />
          Agregar variable
        </Button>
      </div>
    </div>
  );
}
