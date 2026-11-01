import { RefreshCwIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

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
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";
import { STATE_LABELS } from "@/features/prepare/VmSelector";
import { UnitButtons } from "@/features/web/UnitButtons";

import { deleteDns, dnsAction, dnsStatus, refreshRecords } from "./api";
import type { DnsServer } from "./types";

type Props = { server: DnsServer; busy: boolean; onDone(): void };

// El servidor DNS (REQ-17): nombre, estado, IPv4 y zona, los botones de bind9 (REQ-12),
// "Actualizar registros" (REQ-27) y "Eliminar", que pide confirmación con su nombre (REQ-21).
export function DnsServerPanel({ server, busy, onDone }: Props) {
  const { pending, result, run } = useOperation();
  const [confirmOpen, setConfirmOpen] = useState(false);
  // Operación propia en curso, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<"refresh" | "delete" | null>(null);
  const disabled = busy || pending;

  async function perform(kind: "refresh" | "delete", fn: () => Promise<string>) {
    setConfirmOpen(false);
    setActive(kind);
    await run(fn);
    setActive(null);
    onDone();
  }

  // Si se elimina, la pantalla deja de mostrar este panel al recargar: el mensaje va también en un toast.
  async function deleteAndNotify(): Promise<string> {
    const message = await deleteDns();
    toast.success(message);
    return message;
  }

  const details: [string, string][] = [
    ["Nombre", server.name],
    ["Estado", STATE_LABELS[server.state]],
    ["IP", server.ip || "-"],
    ["Zona", server.zone],
  ];

  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        {details.map(([label, value]) => (
          <div key={label} className="flex flex-col gap-0.5">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="font-medium">{value}</dd>
          </div>
        ))}
      </dl>
      <UnitButtons name={server.name} unit="named" busy={disabled} onAction={dnsAction} onStatus={dnsStatus} onDone={onDone}>
        <Button variant="outline" size="sm" disabled={disabled} onClick={() => void perform("refresh", refreshRecords)}>
          {active === "refresh" ? (
            <Spinner data-icon="inline-start" aria-label="Cargando" />
          ) : (
            <RefreshCwIcon data-icon="inline-start" />
          )}
          Actualizar registros
        </Button>
        <Button variant="outline" size="sm" disabled={disabled} onClick={() => setConfirmOpen(true)}>
          {active === "delete" ? <Spinner data-icon="inline-start" aria-label="Cargando" /> : <Trash2Icon data-icon="inline-start" />}
          Eliminar
        </Button>
      </UnitButtons>
      <OperationResult result={result} />

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Eliminar el servidor DNS {server.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              Se elimina la VM {server.name} con su disco diferencial y todos los registros de la zona {server.zone}.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={() => void perform("delete", deleteAndNotify)}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
