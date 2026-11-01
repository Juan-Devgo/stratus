import { CircleAlertIcon, CircleCheckIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { OperationResult } from "@/components/OperationResult";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { VmRole } from "@/features/prepare/types";
import { useOperation } from "@/features/prepare/useOperation";
import { KIND_LABELS, STATE_LABELS } from "@/features/prepare/VmSelector";

import { addRecords, removeRecord, setResolver } from "./api";
import type { DnsVm, RecordResult } from "./types";

type Props = { vms: DnsVm[]; busy: boolean; onDone(): void };

const ROLE_LABELS: Record<VmRole, string> = { vm: "VM", lb: "Balanceador", server: "Servidor de balanceador" };

// Tipo de la VM: el de 005 si es un servidor web; si no, su rol de 004.
function kindLabel(vm: DnsVm): string {
  return KIND_LABELS[vm.kind] || ROLE_LABELS[vm.role];
}

// Las VMs que no son el servidor DNS, con su registro A y su resolvedor. Las que no tienen registro
// llevan una casilla para "Agregar registros" (REQ-23), que muestra el resultado de cada una; las que
// lo tienen, "Quitar registro" con confirmación (REQ-24). El Switch pone o quita el servidor DNS como
// resolvedor de la VM (REQ-31).
export function RecordTable({ vms, busy, onDone }: Props) {
  const { pending, result, run } = useOperation();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [results, setResults] = useState<RecordResult[]>([]);
  // VM cuyo diálogo de quitar registro está abierto, o null.
  const [removing, setRemoving] = useState<string | null>(null);
  // Operación propia en curso, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<string | null>(null);
  const disabled = busy || pending;

  // Solo cuentan las elegidas que siguen sin registro, en el orden de la tabla.
  const chosen = vms.filter((vm) => vm.record_ip === "" && selected.has(vm.name)).map((vm) => vm.name);

  function toggle(name: string, on: boolean) {
    const next = new Set(selected);
    if (on) {
      next.add(name);
    } else {
      next.delete(name);
    }
    setSelected(next);
  }

  async function perform(key: string, fn: () => Promise<string>) {
    setActive(key);
    setResults([]);
    await run(fn);
    setActive(null);
    onDone();
  }

  function add() {
    void perform("add", async () => {
      const res = await addRecords(chosen);
      setResults(res.results);
      setSelected(new Set());
      return res.message;
    });
  }

  function confirmRemove(vm: string) {
    setRemoving(null);
    void perform(`remove:${vm}`, () => removeRecord(vm));
  }

  return (
    <div className="flex flex-col gap-4">
      {vms.length === 0 ? (
        <p className="text-sm text-muted-foreground">No hay otras VMs registradas</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8">
                <span className="sr-only">Seleccionar</span>
              </TableHead>
              <TableHead>Nombre</TableHead>
              <TableHead>Tipo</TableHead>
              <TableHead>Estado</TableHead>
              <TableHead>IP</TableHead>
              <TableHead>Registro</TableHead>
              <TableHead>IP del registro</TableHead>
              <TableHead>Resolvedor</TableHead>
              <TableHead>Acciones</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {vms.map((vm) => (
              <TableRow key={vm.name}>
                <TableCell>
                  {vm.record_ip === "" && (
                    <Checkbox
                      aria-label={`Seleccionar ${vm.name}`}
                      checked={selected.has(vm.name)}
                      disabled={disabled}
                      onCheckedChange={(checked) => toggle(vm.name, checked)}
                    />
                  )}
                </TableCell>
                <TableCell className="font-medium">{vm.name}</TableCell>
                <TableCell>{kindLabel(vm)}</TableCell>
                <TableCell>{STATE_LABELS[vm.state]}</TableCell>
                <TableCell>{vm.ip || "-"}</TableCell>
                <TableCell>{vm.fqdn || "-"}</TableCell>
                <TableCell>{vm.record_ip || "-"}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Switch
                      aria-label={`Usar como resolvedor en ${vm.name}`}
                      checked={vm.resolver}
                      disabled={disabled}
                      onCheckedChange={(checked) =>
                        void perform(`resolver:${vm.name}`, () => setResolver(vm.name, checked))
                      }
                    />
                    {active === `resolver:${vm.name}` && <Spinner aria-label="Cargando" />}
                  </div>
                </TableCell>
                <TableCell>
                  {vm.record_ip !== "" && (
                    <Button variant="outline" size="sm" disabled={disabled} onClick={() => setRemoving(vm.name)}>
                      {active === `remove:${vm.name}` ? (
                        <Spinner data-icon="inline-start" aria-label="Cargando" />
                      ) : (
                        <Trash2Icon data-icon="inline-start" />
                      )}
                      Quitar registro
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <Button disabled={disabled || chosen.length === 0} onClick={add}>
          {active === "add" ? <Spinner data-icon="inline-start" aria-label="Cargando" /> : <PlusIcon data-icon="inline-start" />}
          Agregar registros
        </Button>
        <span className="text-sm text-muted-foreground">
          {chosen.length === 0 ? "Elija las VMs sin registro" : `${chosen.length} VM(s) elegida(s)`}
        </span>
      </div>

      {/* Fuera de la tabla: el diálogo y el resultado siguen visibles aunque la lista se recargue. */}
      <AlertDialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Quitar el registro de {removing}?</AlertDialogTitle>
            <AlertDialogDescription>
              El nombre {removing} deja de resolver en la zona. La VM no cambia.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={() => removing !== null && confirmRemove(removing)}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <OperationResult result={result} />
      {results.length > 0 && (
        <ul aria-label="Resultado por VM" className="flex flex-col gap-1 text-sm">
          {results.map((item) => (
            <li key={item.vm} className={`flex items-center gap-2 ${item.ok ? "" : "text-destructive"}`}>
              {item.ok ? <CircleCheckIcon className="size-4" /> : <CircleAlertIcon className="size-4" />}
              {item.vm}: {item.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
