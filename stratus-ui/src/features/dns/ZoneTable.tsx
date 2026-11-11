import { CircleAlertIcon, RefreshCwIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { IpCell } from "@/components/IpCell";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/features/prepare/api";

import { getZone } from "./api";
import type { ZoneRecord } from "./types";

// version cambia cuando la pantalla termina una operación: la zona se vuelve a leer (REQ-47).
type Props = { version: number };

// Registros que sirve el servidor DNS, leídos de su archivo de zona (REQ-46): tipo, nombre, valor
// y TTL, en el orden de bind9. Se leen al montar, con "Recargar" y con cada nueva version (REQ-47).
// El valor de un registro A lleva su botón de copiar (REQ-43).
export function ZoneTable({ version }: Props) {
  // null hasta la primera lectura correcta.
  const [records, setRecords] = useState<ZoneRecord[] | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  // Número de la última lectura pedida: una respuesta vieja que llega tarde se descarta.
  const latest = useRef(0);

  const load = useCallback(async () => {
    const id = ++latest.current;
    setLoading(true);
    try {
      const data = await getZone();
      if (id === latest.current) {
        setRecords(data);
        setError("");
      }
    } catch (err) {
      if (id === latest.current) {
        setRecords(null);
        setError(err instanceof ApiError ? err.message : "No se pudo leer la zona.");
      }
    } finally {
      if (id === latest.current) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load, version]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
          {loading ? <Spinner data-icon="inline-start" aria-label="Cargando" /> : <RefreshCwIcon data-icon="inline-start" />}
          Recargar
        </Button>
        <span className="text-sm text-muted-foreground">Registros leídos del servidor DNS.</span>
      </div>

      {error && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Zona no disponible</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {records !== null && records.length === 0 && (
        <p className="text-sm text-muted-foreground">La zona no tiene registros</p>
      )}

      {records !== null && records.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Tipo</TableHead>
              <TableHead>Nombre</TableHead>
              <TableHead>Valor</TableHead>
              <TableHead>TTL</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((rec, i) => (
              // Un nombre puede tener varios registros del mismo tipo: la posición completa la llave.
              <TableRow key={`${rec.type}-${rec.name}-${i}`}>
                <TableCell className="font-medium">{rec.type}</TableCell>
                <TableCell>{rec.name}</TableCell>
                <TableCell className="whitespace-normal break-all">
                  {rec.type === "A" ? <IpCell ip={rec.value} /> : rec.value}
                </TableCell>
                <TableCell>{rec.ttl}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
