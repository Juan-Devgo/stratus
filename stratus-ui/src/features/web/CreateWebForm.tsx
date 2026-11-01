import { useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { WarningAlert } from "@/components/WarningAlert";
import { useOperation } from "@/features/prepare/useOperation";
import type { CreateVmInput } from "@/features/prepare/validation";

import { createWeb } from "./api";
import type { CreateWebInput, WebType } from "./types";
import { createWebErrors, type CreateWebErrors } from "./validation";

// busy es verdadero mientras otra operación de la pantalla está en curso: desactiva el botón.
type Props = { busy: boolean; onDone(): void };

// Campos de texto del formulario; los numéricos se guardan como texto y se convierten al enviar.
const FIELDS: { key: keyof CreateVmInput; label: string; type: "text" | "number"; placeholder?: string }[] = [
  { key: "name", label: "Nombre", type: "text", placeholder: "web-a" },
  { key: "memory_mb", label: "Memoria (MB)", type: "number" },
  { key: "cpus", label: "CPUs", type: "number" },
];

const INITIAL: Record<keyof CreateVmInput, string> = { name: "", memory_mb: "1024", cpus: "1" };

// Base UI necesita la lista de ítems en la raíz; el primero (value null) es el texto inicial.
const TYPE_ITEMS: { label: string; value: WebType | null }[] = [
  { label: "Seleccione el tipo", value: null },
  { label: "Apache", value: "apache" },
  { label: "Nginx", value: "nginx" },
];

// Formulario "Crear servidor web": los campos de 001 y el tipo, Apache o Nginx (REQ-1). Si el
// servidor se creó pero no se pudo registrar en DNS, muestra además la advertencia (REQ-5).
export function CreateWebForm({ busy, onDone }: Props) {
  const [values, setValues] = useState(INITIAL);
  const [type, setType] = useState<WebType | null>(null);
  const [errors, setErrors] = useState<CreateWebErrors>({});
  const [warning, setWarning] = useState("");
  const { pending, result, run } = useOperation();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const input: CreateWebInput = {
      name: values.name,
      memory_mb: Number(values.memory_mb),
      cpus: Number(values.cpus),
      type: type ?? "",
    };
    const found = createWebErrors(input);
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    setWarning("");
    await run(async () => {
      const res = await createWeb(input);
      setWarning(res.warning ?? "");
      return res.message;
    });
    onDone();
  }

  return (
    // noValidate: la validación es la de validation.ts, con mensajes en español.
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        {FIELDS.map(({ key, label, type: inputType, placeholder }) => (
          <Field key={key} data-invalid={errors[key] ? true : undefined}>
            <FieldLabel htmlFor={`create-web-${key}`}>{label}</FieldLabel>
            <Input
              id={`create-web-${key}`}
              type={inputType}
              placeholder={placeholder}
              value={values[key]}
              onChange={(event) => setValues({ ...values, [key]: event.target.value })}
              aria-invalid={errors[key] ? true : undefined}
            />
            {errors[key] && <FieldError>{errors[key]}</FieldError>}
          </Field>
        ))}
        <Field data-invalid={errors.type ? true : undefined}>
          <FieldLabel htmlFor="create-web-type">Tipo</FieldLabel>
          <Select items={TYPE_ITEMS} value={type} onValueChange={(next) => setType(next)}>
            <SelectTrigger id="create-web-type" className="min-w-56" aria-invalid={errors.type ? true : undefined}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {TYPE_ITEMS.map((item) => (
                  <SelectItem key={item.value ?? ""} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          {errors.type && <FieldError>{errors.type}</FieldError>}
        </Field>
        <Field orientation="horizontal">
          <Button type="submit" disabled={pending || busy}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Crear servidor web
          </Button>
        </Field>
        <OperationResult result={result} />
        <WarningAlert warning={warning} />
      </FieldGroup>
    </form>
  );
}
