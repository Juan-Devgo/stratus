import { ServerCogIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

import type { ServiceAction, ServiceRow } from "./types";

type Props = {
  services: ServiceRow[];
  // Hay una operación en curso: todos los botones quedan deshabilitados (REQ-29).
  busy: boolean;
  onAction(name: string, action: ServiceAction): void;
  // Estado y Logs muestran su salida en el panel "Salida" de la pantalla (REQ-19, REQ-20).
  onStatus(name: string): void;
  onLogs(name: string): void;
  // Editar abre el diálogo con los valores actuales de la fila (REQ-21).
  onEdit(service: ServiceRow): void;
  // Redesplegar abre el diálogo con el ejecutable actual de la fila (REQ-24).
  onRedeploy(service: ServiceRow): void;
  // Eliminar abre la confirmación que nombra el servicio (REQ-25).
  onDelete(name: string): void;
};

// Las cinco acciones de systemctl de cada fila (REQ-17), en el orden en que se muestran.
const ACTIONS: { label: string; action: ServiceAction }[] = [
  { label: "Iniciar", action: "start" },
  { label: "Detener", action: "stop" },
  { label: "Reiniciar", action: "restart" },
  { label: "Habilitar", action: "enable" },
  { label: "Deshabilitar", action: "disable" },
];

// Servicios creados por Stratus en la VM, con su estado activo y habilitado tal como los informa
// systemd (REQ-16), un botón por acción de systemctl y los botones Estado, Logs, Editar, Redesplegar y Eliminar.
export function ServiceTable({ services, busy, onAction, onStatus, onLogs, onEdit, onRedeploy, onDelete }: Props) {
  if (services.length === 0) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ServerCogIcon />
          </EmptyMedia>
          <EmptyTitle>Sin servicios de Stratus</EmptyTitle>
          <EmptyDescription>Seleccione una VM para ver los servicios creados con esta consola.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Servicio</TableHead>
          <TableHead>Descripción</TableHead>
          <TableHead>Estado</TableHead>
          <TableHead>Habilitado</TableHead>
          <TableHead>Restart</TableHead>
          <TableHead>RestartSec</TableHead>
          <TableHead>Acciones</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {services.map((service) => (
          <TableRow key={service.name}>
            <TableCell>{service.name}</TableCell>
            <TableCell>{service.description}</TableCell>
            <TableCell>{service.active_state}</TableCell>
            <TableCell>{service.enabled_state}</TableCell>
            <TableCell>{service.restart}</TableCell>
            <TableCell>{service.restart_sec}</TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-1">
                {ACTIONS.map(({ label, action }) => (
                  <Button
                    key={action}
                    variant="outline"
                    size="sm"
                    aria-label={`${label} ${service.name}`}
                    disabled={busy}
                    onClick={() => onAction(service.name, action)}
                  >
                    {label}
                  </Button>
                ))}
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Estado ${service.name}`}
                  disabled={busy}
                  onClick={() => onStatus(service.name)}
                >
                  Estado
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Logs ${service.name}`}
                  disabled={busy}
                  onClick={() => onLogs(service.name)}
                >
                  Logs
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Editar ${service.name}`}
                  disabled={busy}
                  onClick={() => onEdit(service)}
                >
                  Editar
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Redesplegar ${service.name}`}
                  disabled={busy}
                  onClick={() => onRedeploy(service)}
                >
                  Redesplegar
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Eliminar ${service.name}`}
                  disabled={busy}
                  onClick={() => onDelete(service.name)}
                >
                  Eliminar
                </Button>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
