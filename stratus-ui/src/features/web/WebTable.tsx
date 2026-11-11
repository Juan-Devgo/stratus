import { GlobeIcon, Trash2Icon, UploadIcon } from "lucide-react";
import { Fragment, useState } from "react";

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
import { IpCell } from "@/components/IpCell";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { WarningAlert } from "@/components/WarningAlert";
import { useOperation } from "@/features/prepare/useOperation";
import { STATE_LABELS } from "@/features/prepare/VmSelector";

import { deleteWeb, webAction, webStatus } from "./api";
import { InstallPageDialog } from "./InstallPageDialog";
import type { WebServer, WebType } from "./types";
import { UnitButtons } from "./UnitButtons";

type Props = { servers: WebServer[]; busy: boolean; onDone(): void };

const TYPE_LABELS: Record<WebType, string> = { apache: "Apache", nginx: "Nginx" };
const UNITS: Record<WebType, string> = { apache: "apache2", nginx: "nginx" };

// Servidores web registrados (REQ-6): nombre, tipo, estado, IPv4, FQDN y balanceador ("-" si no
// hay). Debajo de cada fila van sus botones: los de systemctl, "Instalar página" y "Eliminar", que
// pide confirmación con el nombre de la VM (REQ-8).
export function WebTable({ servers, busy, onDone }: Props) {
  const { pending, result, run } = useOperation();
  // VM cuyo diálogo de eliminar o de instalar página está abierto, o null.
  const [deleting, setDeleting] = useState<string | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);
  // VM que se está eliminando, para mostrar el Spinner en su botón.
  const [active, setActive] = useState<string | null>(null);
  const [warning, setWarning] = useState("");

  async function confirmDelete(vm: string) {
    setDeleting(null);
    setActive(vm);
    setWarning("");
    await run(async () => {
      const res = await deleteWeb(vm);
      setWarning(res.warning ?? "");
      return res.message;
    });
    setActive(null);
    onDone();
  }

  if (servers.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <GlobeIcon />
            </EmptyMedia>
            <EmptyTitle>Sin servidores web</EmptyTitle>
            <EmptyDescription>Cree un servidor web con el formulario de abajo.</EmptyDescription>
          </EmptyHeader>
        </Empty>
        <OperationResult result={result} />
        <WarningAlert warning={warning} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Nombre</TableHead>
            <TableHead>Tipo</TableHead>
            <TableHead>Estado</TableHead>
            <TableHead>IP</TableHead>
            <TableHead>FQDN</TableHead>
            <TableHead>Balanceador</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {servers.map((server) => (
            <Fragment key={server.name}>
              <TableRow className="border-b-0">
                <TableCell className="font-medium">{server.name}</TableCell>
                <TableCell>{TYPE_LABELS[server.type]}</TableCell>
                <TableCell>{STATE_LABELS[server.state]}</TableCell>
                <TableCell>
                  <IpCell ip={server.ip} />
                </TableCell>
                <TableCell>{server.fqdn || "-"}</TableCell>
                <TableCell>{server.lb || "-"}</TableCell>
              </TableRow>
              <TableRow>
                <TableCell colSpan={6} className="whitespace-normal">
                  <UnitButtons
                    name={server.name}
                    unit={UNITS[server.type]}
                    busy={busy || pending}
                    onAction={(action) => webAction(server.name, action)}
                    onStatus={() => webStatus(server.name)}
                    onDone={onDone}
                  >
                    <Button variant="outline" size="sm" disabled={busy || pending} onClick={() => setInstalling(server.name)}>
                      <UploadIcon data-icon="inline-start" />
                      Instalar página
                    </Button>
                    <Button variant="outline" size="sm" disabled={busy || pending} onClick={() => setDeleting(server.name)}>
                      {active === server.name ? (
                        <Spinner data-icon="inline-start" aria-label="Cargando" />
                      ) : (
                        <Trash2Icon data-icon="inline-start" />
                      )}
                      Eliminar
                    </Button>
                  </UnitButtons>
                </TableCell>
              </TableRow>
            </Fragment>
          ))}
        </TableBody>
      </Table>

      {/* Fuera de la tabla: el diálogo y el resultado siguen visibles aunque la lista se recargue. */}
      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>¿Eliminar el servidor web {deleting}?</AlertDialogTitle>
            <AlertDialogDescription>
              Se elimina la VM {deleting} con su disco diferencial y su registro DNS. El disco base no cambia.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={() => deleting !== null && void confirmDelete(deleting)}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <InstallPageDialog vm={installing} onClose={() => setInstalling(null)} onDone={onDone} />
      <OperationResult result={result} />
      <WarningAlert warning={warning} />
    </div>
  );
}
