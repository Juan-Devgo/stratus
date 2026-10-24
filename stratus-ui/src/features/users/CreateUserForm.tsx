import { useState, type FormEvent } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import type { Vm } from "@/features/prepare/types";
import { useOperation } from "@/features/prepare/useOperation";

import { createUser } from "./api";
import type { CreateUserInput } from "./types";
import { UserKeyDownload } from "./UserKeyDownload";
import { createUserErrors, type CreateUserFormErrors } from "./validation";

type Props = { vm: Vm | null; onDone(): void };

const INITIAL = { username: "", sudo: false, ssh_key: false, password: "", confirmation: "" };

// Formulario de creación (REQ-3): nombre, sudo y llaves SSH. Con llaves no hay campos de contraseña
// (REQ-8); sin llaves se piden la contraseña y su confirmación (REQ-7).
export function CreateUserForm({ vm, onDone }: Props) {
  const [values, setValues] = useState(INITIAL);
  const [errors, setErrors] = useState<CreateUserFormErrors>({});
  // Usuario creado con llaves cuya llave privada aún se puede descargar.
  const [keyFor, setKeyFor] = useState<{ vm: Vm; username: string } | null>(null);
  const { pending, result, run } = useOperation();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!vm) {
      return;
    }
    const found = createUserErrors(values);
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    const input: CreateUserInput = { username: values.username, sudo: values.sudo, ssh_key: values.ssh_key };
    if (!values.ssh_key) {
      input.password = values.password;
    }
    // La contraseña sale del formulario en cuanto se envía; no se conserva en pantalla (REQ-9).
    setValues({ ...values, password: "", confirmation: "" });
    setKeyFor(null);
    await run(async () => {
      const created = await createUser(vm.name, input);
      if (created.key_available) {
        setKeyFor({ vm, username: input.username });
      }
      return created.message;
    });
    onDone();
  }

  function invalid(field: keyof CreateUserFormErrors) {
    return errors[field] ? true : undefined;
  }

  return (
    // noValidate: la validación es la de validation.ts, con mensajes en español.
    <form onSubmit={handleSubmit} noValidate>
      <FieldGroup>
        <Field data-invalid={invalid("username")}>
          <FieldLabel htmlFor="create-user-username">Nombre de usuario</FieldLabel>
          <Input
            id="create-user-username"
            placeholder="ana"
            autoComplete="off"
            value={values.username}
            onChange={(event) => setValues({ ...values, username: event.target.value })}
            aria-invalid={invalid("username")}
          />
          {errors.username && <FieldError>{errors.username}</FieldError>}
        </Field>

        <Field orientation="horizontal">
          <Checkbox
            id="create-user-sudo"
            checked={values.sudo}
            onCheckedChange={(on) => setValues({ ...values, sudo: on })}
          />
          <FieldLabel htmlFor="create-user-sudo" className="font-normal">
            Privilegios de sudo
          </FieldLabel>
        </Field>

        <Field orientation="horizontal">
          <Checkbox
            id="create-user-ssh-key"
            checked={values.ssh_key}
            onCheckedChange={(on) => setValues({ ...values, ssh_key: on, password: "", confirmation: "" })}
          />
          <FieldLabel htmlFor="create-user-ssh-key" className="font-normal">
            Generar llaves SSH
          </FieldLabel>
        </Field>

        {!values.ssh_key && (
          <>
            <Field data-invalid={invalid("password")}>
              <FieldLabel htmlFor="create-user-password">Contraseña</FieldLabel>
              <Input
                id="create-user-password"
                type="password"
                autoComplete="new-password"
                value={values.password}
                onChange={(event) => setValues({ ...values, password: event.target.value })}
                aria-invalid={invalid("password")}
              />
              {errors.password && <FieldError>{errors.password}</FieldError>}
            </Field>
            <Field data-invalid={invalid("confirmation")}>
              <FieldLabel htmlFor="create-user-confirmation">Confirmar contraseña</FieldLabel>
              <Input
                id="create-user-confirmation"
                type="password"
                autoComplete="new-password"
                value={values.confirmation}
                onChange={(event) => setValues({ ...values, confirmation: event.target.value })}
                aria-invalid={invalid("confirmation")}
              />
              {errors.confirmation && <FieldError>{errors.confirmation}</FieldError>}
              <FieldDescription>
                Sirve en la consola de la VM y con su; el acceso por SSH con contraseña sigue deshabilitado.
              </FieldDescription>
            </Field>
          </>
        )}

        <Field orientation="horizontal">
          <Button type="submit" disabled={vm === null || pending}>
            {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
            Crear usuario
          </Button>
        </Field>
        {vm === null && <FieldDescription>Seleccione una VM para continuar</FieldDescription>}

        <OperationResult result={result} />
        {keyFor && <UserKeyDownload vm={keyFor.vm} username={keyFor.username} />}
      </FieldGroup>
    </form>
  );
}
