import { ServerOffIcon, Trash2Icon } from "lucide-react";
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
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useOperation } from "@/features/prepare/useOperation";

import { removeServer } from "./api";
import type { LbServer } from "./types";

type Props = { servers: LbServer[]; lbOff: boolean; lb: string; onDone(): void };

// Servidores del balanceador seleccionado (REQ-11, REQ-20): nombre, VM, última IP interna, puerto y
// estado de HAProxy ("-" si no se leyó). "Quitar" pide confirmación porque la VM se reinicia.
export function ServerTable({ servers, lbOff, lb, onDone }: Props) {
  const { pending, result, run } = useOperation();
  // VM cuyo diálogo de confirmación está abierto, o null.
  const [removing, setRemoving] = useState<string | null>(null);
  // VM que se está quitando, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<string | null>(null);

  async function confirm(vm: string) {
    setRemoving(null);
    setActive(vm);
    await run(() => removeServer(lb, vm));
    setActive(null);
    onDone();
  }

  return (
    <div className="flex flex-col gap-4">
      {lbOff && <p className="text-sm text-muted-foreground">El balanceador está apagado; los datos son los últimos conocidos</p>}
      {servers.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerOffIcon />
            </EmptyMedia>
            <EmptyTitle>Sin servidores</EmptyTitle>
            <EmptyDescription>Asigne una VM con el formulario de abajo.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Servidor</TableHead>
              <TableHead>VM</TableHead>
              <TableHead>IP interna</TableHead>
              <TableHead>Puerto</TableHead>
              <TableHead>Estado</TableHead>
              <TableHead>Acciones</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {servers.map((server) => (
              <TableRow key={server.name}>
                <TableCell>{server.name}</TableCell>
                <TableCell>{server.vm}</TableCell>
                <TableCell>{server.ip}</TableCell>
                <TableCell>{server.port}</TableCell>
                <TableCell>{server.state}</TableCell>
                <TableCell>
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={`Quitar ${server.vm}`}
                    disabled={pending}
                    onClick={() => setRemoving(server.vm)}
                  >
                    {active === server.vm ? (
                      <Spinner data-icon="inline-start" aria-label="Cargando" />
                    ) : (
                      <Trash2Icon data-icon="inline-start" />
                    )}
                    Quitar
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {/* Fuera de la tabla: el diálogo y el resultado siguen visibles aunque la lista se recargue o quede vacía. */}
      <AlertDialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Quitar {removing} de {lb}?</AlertDialogTitle>
            <AlertDialogDescription>
              Se quita su línea del balanceador y sus llaves de la cuenta jump, y la VM {removing} vuelve a la red en
              puente y se reinicia si estaba encendida.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={() => removing !== null && void confirm(removing)}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <OperationResult result={result} />
    </div>
  );
}
