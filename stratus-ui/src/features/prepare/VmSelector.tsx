import { Badge } from "@/components/ui/badge";
import { Field, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

import type { Vm, VmState } from "./types";

export const STATE_LABELS: Record<VmState, string> = {
  running: "Encendida",
  poweroff: "Apagada",
  unknown: "Desconocido",
};

// Texto de una VM en el selector: "<nombre> - <Estado> - <ip|->" y, si es un balanceador, " - Balanceador".
function vmLabel(vm: Vm): string {
  const base = `${vm.name} - ${STATE_LABELS[vm.state]} - ${vm.ip || "-"}`;
  return vm.role === "lb" ? `${base} - Balanceador` : base;
}

type Props = {
  vms: Vm[];
  value: string | null;
  onChange(name: string | null): void;
};

// Selector de la VM sobre la que actúan todas las demás operaciones (REQ-2).
export function VmSelector({ vms, value, onChange }: Props) {
  // Base UI necesita la lista de ítems en la raíz; el primero (value null) es el texto inicial.
  const items = [
    { label: vms.length > 0 ? "Seleccione una VM" : "No hay VMs registradas", value: null },
    ...vms.map((vm) => ({ label: vmLabel(vm), value: vm.name })),
  ];
  const selected = vms.find((vm) => vm.name === value);

  return (
    <Field orientation="horizontal">
      <FieldLabel htmlFor="vm-selector">VM de trabajo</FieldLabel>
      <Select items={items} value={value} onValueChange={(next) => onChange(next)}>
        <SelectTrigger id="vm-selector" className="min-w-56">
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
      {selected && (
        <Badge variant={selected.state === "running" ? "default" : "secondary"}>{STATE_LABELS[selected.state]}</Badge>
      )}
    </Field>
  );
}
