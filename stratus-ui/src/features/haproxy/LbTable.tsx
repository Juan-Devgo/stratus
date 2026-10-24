import { ServerIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { STATE_LABELS } from "@/features/prepare/VmSelector";

import type { Lb } from "./types";

type Props = { lbs: Lb[]; selected: string | null; onSelect(name: string): void };

// Balanceadores registrados (REQ-5): nombre, estado, IPv4 en puente ("-" si está apagado), subred
// interna, puerto del frontend y número de servidores. Cada fila tiene un botón para seleccionarla.
export function LbTable({ lbs, selected, onSelect }: Props) {
  if (lbs.length === 0) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ServerIcon />
          </EmptyMedia>
          <EmptyTitle>Sin balanceadores</EmptyTitle>
          <EmptyDescription>Cree un balanceador con el formulario de abajo.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Nombre</TableHead>
          <TableHead>Estado</TableHead>
          <TableHead>IP</TableHead>
          <TableHead>Subred</TableHead>
          <TableHead>Puerto</TableHead>
          <TableHead>Servidores</TableHead>
          <TableHead>Acciones</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {lbs.map((lb) => (
          <TableRow key={lb.name} data-state={lb.name === selected ? "selected" : undefined}>
            <TableCell>{lb.name}</TableCell>
            <TableCell>{STATE_LABELS[lb.state]}</TableCell>
            <TableCell>{lb.ip || "-"}</TableCell>
            <TableCell>{lb.subnet}</TableCell>
            <TableCell>{lb.frontend_port}</TableCell>
            <TableCell>{lb.server_count}</TableCell>
            <TableCell>
              <Button
                variant={lb.name === selected ? "default" : "outline"}
                size="sm"
                aria-label={`Seleccionar ${lb.name}`}
                aria-pressed={lb.name === selected}
                onClick={() => onSelect(lb.name)}
              >
                {lb.name === selected ? "Seleccionado" : "Seleccionar"}
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
