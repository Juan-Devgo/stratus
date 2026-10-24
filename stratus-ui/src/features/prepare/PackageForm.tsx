import { useState } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";

import { installPackage, removePackage } from "./api";
import type { Vm } from "./types";
import { useOperation } from "./useOperation";
import { validatePackage } from "./validation";

type Props = { vm: Vm | null; catalog: string[]; onDone(): void };

type Action = "install" | "remove";

// Instalar o eliminar una aplicación en la VM seleccionada (REQ-4). El catálogo solo llena el
// campo de texto; también se puede escribir cualquier paquete que cumpla la lista permitida.
export function PackageForm({ vm, catalog, onDone }: Props) {
  const [pkg, setPkg] = useState("");
  const [error, setError] = useState<string | null>(null);
  const { pending, result, run } = useOperation();
  // Acción en curso, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<Action | null>(null);
  const disabled = vm === null || pending;

  // Base UI necesita la lista de ítems en la raíz; el primero (value null) es el texto inicial.
  const items = [
    { label: "Elija una aplicación", value: null },
    ...catalog.map((name) => ({ label: name, value: name })),
  ];

  async function execute(action: Action, fn: (name: string, pkg: string) => Promise<string>) {
    if (!vm) {
      return;
    }
    const found = validatePackage(pkg);
    setError(found);
    if (found) {
      return;
    }
    setActive(action);
    await run(() => fn(vm.name, pkg));
    setActive(null);
    onDone();
  }

  function label(action: Action, text: string) {
    return (
      <>
        {active === action && <Spinner data-icon="inline-start" aria-label="Cargando" />}
        {text}
      </>
    );
  }

  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="package-catalog">Catálogo</FieldLabel>
        <Select
          items={items}
          // El selector refleja el campo solo si el texto coincide con una entrada del catálogo.
          value={catalog.includes(pkg) ? pkg : null}
          onValueChange={(next) => {
            if (next) {
              setPkg(next);
              setError(null);
            }
          }}
        >
          <SelectTrigger id="package-catalog" className="min-w-56">
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
        <FieldLabel htmlFor="package-name">Paquete</FieldLabel>
        <Input
          id="package-name"
          placeholder="nginx"
          value={pkg}
          onChange={(event) => setPkg(event.target.value)}
          aria-invalid={error ? true : undefined}
        />
        {error && <FieldError>{error}</FieldError>}
      </Field>
      <Field orientation="horizontal">
        <Button disabled={disabled} onClick={() => execute("install", installPackage)}>
          {label("install", "Instalar")}
        </Button>
        <Button variant="destructive" disabled={disabled} onClick={() => execute("remove", removePackage)}>
          {label("remove", "Eliminar")}
        </Button>
      </Field>
      {vm === null && <FieldDescription>Seleccione una VM para continuar</FieldDescription>}
      <OperationResult result={result} />
    </FieldGroup>
  );
}
