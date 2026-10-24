import { useEffect, useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import type { Vm } from "@/features/prepare/types";
import { useOperation } from "@/features/prepare/useOperation";

import { createService } from "./api";
import { EnvVarFields } from "./EnvVarFields";
import { StateWarning } from "./StateWarning";
import type { EnvVar, Restart } from "./types";
import { createServiceErrors, type ServiceFormErrors } from "./validation";

type Props = { vm: Vm | null; busy: boolean; onBusyChange(busy: boolean): void; onDone(): void };

// Opciones de Restart: el valor por defecto es on-failure y RestartSec 5 (REQ-8).
const RESTART_ITEMS: { label: string; value: Restart }[] = [
  { label: "on-failure", value: "on-failure" },
  { label: "always", value: "always" },
  { label: "no", value: "no" },
];

interface Values {
  name: string;
  description: string;
  executable: string;
  args: string;
  restart: Restart;
  restart_sec: string;
  env: EnvVar[];
  archive: File | null;
}

const INITIAL: Values = {
  name: "",
  description: "",
  executable: "",
  args: "",
  restart: "on-failure",
  restart_sec: "5",
  env: [],
  archive: null,
};

// Formulario de creación (REQ-1): nombre, descripción, archivo .tar.gz, ejecutable, argumentos,
// Restart, RestartSec y variables de entorno. Valida con validation.ts antes de enviar (P3); el
// resultado muestra el estado leído tras iniciar y avisa si no es "active" (REQ-15).
export function CreateServiceForm({ vm, busy, onBusyChange, onDone }: Props) {
  const [values, setValues] = useState<Values>(INITIAL);
  const [errors, setErrors] = useState<ServiceFormErrors>({});
  const [activeState, setActiveState] = useState<string | undefined>();
  // Nombre del servicio del último resultado, para el aviso de estado.
  const [created, setCreated] = useState("");
  const { pending, result, run } = useOperation();

  // Avisa a la pantalla que hay una operación en curso: deshabilita los botones de las filas.
  useEffect(() => {
    onBusyChange(pending);
  }, [pending, onBusyChange]);

  const disabled = busy || pending;

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!vm) {
      return;
    }
    const found = createServiceErrors(values);
    setErrors(found);
    if (Object.keys(found).length > 0 || !values.archive) {
      return;
    }
    const { archive, restart_sec, env, ...rest } = values;
    const input = { ...rest, restart_sec: Number(restart_sec), env };
    // Los valores secretos salen del formulario en cuanto se envían (REQ-13).
    setValues({ ...values, env: values.env.map((row) => ({ ...row, value: "" })) });
    setActiveState(undefined);
    await run(async () => {
      const done = await createService(vm.name, input, archive);
      setCreated(input.name);
      setActiveState(done.active_state);
      return done.message;
    });
    onDone();
  }

  function invalid(field: string) {
    return errors[field] ? true : undefined;
  }

  return (
    // noValidate: la validación es la de validation.ts, con mensajes en español.
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        <Field data-invalid={invalid("name")}>
          <FieldLabel htmlFor="create-service-name">Nombre del servicio</FieldLabel>
          <Input
            id="create-service-name"
            placeholder="demian-lee"
            autoComplete="off"
            value={values.name}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, name: event.target.value })}
            aria-invalid={invalid("name")}
          />
          {errors.name && <FieldError>{errors.name}</FieldError>}
        </Field>

        <Field data-invalid={invalid("description")}>
          <FieldLabel htmlFor="create-service-description">Descripción</FieldLabel>
          <Input
            id="create-service-description"
            placeholder="Servidor de producción"
            autoComplete="off"
            value={values.description}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, description: event.target.value })}
            aria-invalid={invalid("description")}
          />
          {errors.description && <FieldError>{errors.description}</FieldError>}
        </Field>

        <Field data-invalid={invalid("archive")}>
          <FieldLabel htmlFor="create-service-archive">Archivo de la aplicación (.tar.gz)</FieldLabel>
          <Input
            id="create-service-archive"
            type="file"
            accept=".gz,.tgz"
            disabled={disabled}
            onChange={(event) => setValues({ ...values, archive: event.target.files?.[0] ?? null })}
            aria-invalid={invalid("archive")}
          />
          {errors.archive && <FieldError>{errors.archive}</FieldError>}
          <FieldDescription>
            Hasta 200 MB. Su contenido se extrae en /opt/&lt;nombre&gt; con el usuario del servicio como dueño.
          </FieldDescription>
        </Field>

        <Field data-invalid={invalid("executable")}>
          <FieldLabel htmlFor="create-service-executable">Ejecutable</FieldLabel>
          <Input
            id="create-service-executable"
            placeholder="demian-lee"
            autoComplete="off"
            value={values.executable}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, executable: event.target.value })}
            aria-invalid={invalid("executable")}
          />
          {errors.executable && <FieldError>{errors.executable}</FieldError>}
          <FieldDescription>Ruta relativa a /opt/&lt;nombre&gt;; debe estar en el archivo.</FieldDescription>
        </Field>

        <Field data-invalid={invalid("args")}>
          <FieldLabel htmlFor="create-service-args">Argumentos</FieldLabel>
          <Input
            id="create-service-args"
            placeholder="--port 8080"
            autoComplete="off"
            value={values.args}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, args: event.target.value })}
            aria-invalid={invalid("args")}
          />
          {errors.args && <FieldError>{errors.args}</FieldError>}
          <FieldDescription>Opcionales: hasta 16 tokens separados por un espacio.</FieldDescription>
        </Field>

        <Field data-invalid={invalid("restart")}>
          <FieldLabel htmlFor="create-service-restart">Restart</FieldLabel>
          <Select
            items={RESTART_ITEMS}
            value={values.restart}
            disabled={disabled}
            onValueChange={(next) => next && setValues({ ...values, restart: next })}
          >
            <SelectTrigger id="create-service-restart" className="min-w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {RESTART_ITEMS.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          {errors.restart && <FieldError>{errors.restart}</FieldError>}
        </Field>

        <Field data-invalid={invalid("restart_sec")}>
          <FieldLabel htmlFor="create-service-restart-sec">RestartSec</FieldLabel>
          <Input
            id="create-service-restart-sec"
            inputMode="numeric"
            autoComplete="off"
            value={values.restart_sec}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, restart_sec: event.target.value })}
            aria-invalid={invalid("restart_sec")}
          />
          {errors.restart_sec && <FieldError>{errors.restart_sec}</FieldError>}
          <FieldDescription>Segundos antes de reiniciar, de 0 a 3600.</FieldDescription>
        </Field>

        <FieldSet>
          <FieldLegend variant="label">Variables de entorno</FieldLegend>
          <FieldDescription>
            Los valores se guardan en /etc/&lt;nombre&gt;.env (modo 600) y no se vuelven a mostrar.
          </FieldDescription>
          <EnvVarFields
            rows={values.env}
            onChange={(env) => setValues({ ...values, env })}
            errors={errors}
            disabled={disabled}
          />
        </FieldSet>

        <Field orientation="horizontal">
          <Button type="submit" disabled={vm === null || disabled}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Crear servicio
          </Button>
        </Field>
        {vm === null && <FieldDescription>Seleccione una VM para continuar</FieldDescription>}

        <OperationResult result={result} />
        {result?.ok && <StateWarning name={created} state={activeState} />}
      </FieldGroup>
    </form>
  );
}
