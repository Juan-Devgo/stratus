import { useState } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";

import { editService } from "./api";
import { EnvVarFields, type EnvRow } from "./EnvVarFields";
import { StateWarning } from "./StateWarning";
import type { Restart, ServiceRow } from "./types";
import { editServiceErrors, type ServiceFormErrors } from "./validation";

type Props = { vm: string; service: ServiceRow | null; onClose(): void; onDone(): void };

const RESTART_ITEMS: { label: string; value: Restart }[] = [
  { label: "on-failure", value: "on-failure" },
  { label: "always", value: "always" },
  { label: "no", value: "no" },
];

// Edición de un servicio (REQ-21, REQ-22, REQ-23): los campos vienen con los valores actuales de la
// fila y el nombre no se puede cambiar. Las variables solo se reescriben con la casilla
// "Reemplazar variables de entorno"; el resultado se muestra en el mismo diálogo, que se cierra con
// "Cerrar".
export function EditServiceDialog({ vm, service, onClose, onDone }: Props) {
  return (
    <Dialog open={service !== null} onOpenChange={(open) => !open && onClose()} disablePointerDismissal>
      {/* La clave reinicia el formulario y el resultado con cada servicio. */}
      {service !== null && <EditContent key={service.name} vm={vm} service={service} onClose={onClose} onDone={onDone} />}
    </Dialog>
  );
}

function EditContent({ vm, service, onClose, onDone }: Props & { service: ServiceRow }) {
  const [values, setValues] = useState({
    description: service.description,
    executable: service.executable,
    args: service.args,
    restart: service.restart,
    restart_sec: String(service.restart_sec),
  });
  const [replaceEnv, setReplaceEnv] = useState(false);
  const [env, setEnv] = useState<EnvRow[]>([]);
  const [errors, setErrors] = useState<ServiceFormErrors>({});
  const [activeState, setActiveState] = useState<string | undefined>();
  const { pending, result, run } = useOperation();
  // Con un resultado a la vista el formulario queda solo de lectura.
  const disabled = pending || result !== null;

  function toggleReplace(on: boolean) {
    setReplaceEnv(on);
    // Las claves actuales vuelven con el valor vacío y de solo lectura: se escribe cada valor otra vez.
    setEnv(on ? service.env_keys.map((key) => ({ key, value: "", locked: true })) : []);
  }

  async function save() {
    const form = { ...values, replace_env: replaceEnv, env };
    const found = editServiceErrors(form);
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    const input = {
      description: values.description,
      executable: values.executable,
      args: values.args,
      restart: values.restart,
      restart_sec: Number(values.restart_sec),
      replace_env: replaceEnv,
      env: replaceEnv ? env.map(({ key, value }) => ({ key, value })) : [],
    };
    await run(async () => {
      const done = await editService(vm, service.name, input);
      setActiveState(done.active_state);
      return done.message;
    });
    onDone();
  }

  function invalid(field: string) {
    return errors[field] ? true : undefined;
  }

  return (
    <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl" showCloseButton={false}>
      <DialogHeader>
        <DialogTitle>Editar el servicio {service.name}</DialogTitle>
        <DialogDescription>
          Los cambios se aplican en la VM {vm}. El nombre no se puede cambiar. Si el servicio está en ejecución, se
          reinicia.
        </DialogDescription>
      </DialogHeader>

      <FieldGroup>
        <Field data-invalid={invalid("description")}>
          <FieldLabel htmlFor="edit-service-description">Descripción</FieldLabel>
          <Input
            id="edit-service-description"
            autoComplete="off"
            value={values.description}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, description: event.target.value })}
            aria-invalid={invalid("description")}
          />
          {errors.description && <FieldError>{errors.description}</FieldError>}
        </Field>

        <Field data-invalid={invalid("executable")}>
          <FieldLabel htmlFor="edit-service-executable">Ejecutable</FieldLabel>
          <Input
            id="edit-service-executable"
            autoComplete="off"
            value={values.executable}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, executable: event.target.value })}
            aria-invalid={invalid("executable")}
          />
          {errors.executable && <FieldError>{errors.executable}</FieldError>}
          <FieldDescription>Ruta relativa a /opt/{service.name}; debe existir en la VM.</FieldDescription>
        </Field>

        <Field data-invalid={invalid("args")}>
          <FieldLabel htmlFor="edit-service-args">Argumentos</FieldLabel>
          <Input
            id="edit-service-args"
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
          <FieldLabel htmlFor="edit-service-restart">Restart</FieldLabel>
          <Select
            items={RESTART_ITEMS}
            value={values.restart}
            disabled={disabled}
            onValueChange={(next) => next && setValues({ ...values, restart: next })}
          >
            <SelectTrigger id="edit-service-restart" className="min-w-40">
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
          <FieldLabel htmlFor="edit-service-restart-sec">RestartSec</FieldLabel>
          <Input
            id="edit-service-restart-sec"
            inputMode="numeric"
            autoComplete="off"
            value={values.restart_sec}
            disabled={disabled}
            onChange={(event) => setValues({ ...values, restart_sec: event.target.value })}
            aria-invalid={invalid("restart_sec")}
          />
          {errors.restart_sec && <FieldError>{errors.restart_sec}</FieldError>}
        </Field>

        <Field orientation="horizontal">
          <Checkbox id="edit-service-replace-env" checked={replaceEnv} disabled={disabled} onCheckedChange={toggleReplace} />
          <FieldLabel htmlFor="edit-service-replace-env" className="font-normal">
            Reemplazar variables de entorno
          </FieldLabel>
        </Field>

        {replaceEnv ? (
          <>
            <FieldDescription>
              Escriba de nuevo el valor de cada variable: el archivo se reescribe completo con las filas que queden.
            </FieldDescription>
            <EnvVarFields rows={env} onChange={setEnv} errors={errors} disabled={disabled} />
          </>
        ) : (
          <Field>
            <FieldLabel>Variables de entorno actuales</FieldLabel>
            <div className="flex flex-wrap gap-1">
              {service.env_keys.length === 0 ? (
                <span className="text-sm text-muted-foreground">Sin variables</span>
              ) : (
                service.env_keys.map((key) => (
                  <Badge key={key} variant="secondary">
                    {key}
                  </Badge>
                ))
              )}
            </div>
            <FieldDescription>Los valores no se muestran. Sin reemplazo, el archivo queda como está.</FieldDescription>
          </Field>
        )}

        <OperationResult result={result} />
        {result?.ok && <StateWarning name={service.name} state={activeState} />}
      </FieldGroup>

      <DialogFooter>
        {result === null ? (
          <>
            <Button variant="outline" disabled={pending} onClick={onClose}>
              Cancelar
            </Button>
            <Button disabled={pending} onClick={() => void save()}>
              {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
              Guardar
            </Button>
          </>
        ) : (
          <Button onClick={onClose}>Cerrar</Button>
        )}
      </DialogFooter>
    </DialogContent>
  );
}
