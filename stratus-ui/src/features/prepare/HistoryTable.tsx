import { HistoryIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

import type { OperationRow } from "./types";

const RESULT_LABELS: Record<OperationRow["result"], string> = {
  ok: "Éxito",
  error: "Fallo",
  timeout: "Tiempo agotado",
};

type Props = { operations: OperationRow[] };

// Historial del módulo (REQ-18): llega ya ordenado del backend, la más reciente primero.
export function HistoryTable({ operations }: Props) {
  if (operations.length === 0) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <HistoryIcon />
          </EmptyMedia>
          <EmptyTitle>Sin operaciones registradas</EmptyTitle>
          <EmptyDescription>Las operaciones sobre las VMs aparecerán aquí al terminar.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Fecha</TableHead>
          <TableHead>Operación</TableHead>
          <TableHead>VM</TableHead>
          <TableHead>Resultado</TableHead>
          <TableHead>Código</TableHead>
          <TableHead>
            <span className="sr-only">Detalle</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      {/* Cada operación es su propio <tbody> para que la fila y su detalle se abran juntos. */}
      {operations.map((op) => (
        <Collapsible key={op.id} render={<TableBody className="border-b" />}>
          <TableRow>
            <TableCell>{new Date(op.created_at).toLocaleString("es-CO")}</TableCell>
            <TableCell>{op.operation}</TableCell>
            <TableCell>{op.vm}</TableCell>
            <TableCell>
              <Badge variant={op.result === "ok" ? "secondary" : "destructive"}>{RESULT_LABELS[op.result]}</Badge>
            </TableCell>
            <TableCell>{op.exit_code}</TableCell>
            <TableCell>
              <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>Ver stderr</CollapsibleTrigger>
            </TableCell>
          </TableRow>
          <CollapsibleContent render={<TableRow />}>
            <TableCell colSpan={6} className="whitespace-normal">
              <div className="flex flex-col gap-1">
                <span className="font-mono text-xs">
                  Comando: {[op.command, ...op.args].join(" ")}
                </span>
                <span>Código de salida: {op.exit_code}</span>
                <pre className="font-mono text-xs whitespace-pre-wrap">{op.stderr || "Sin salida en stderr"}</pre>
              </div>
            </TableCell>
          </CollapsibleContent>
        </Collapsible>
      ))}
    </Table>
  );
}
