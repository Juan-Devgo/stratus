import { CircleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { getDnsHistory, getDnsOverview } from "@/features/dns/api";
import { CreateDnsForm } from "@/features/dns/CreateDnsForm";
import { DnsServerPanel } from "@/features/dns/DnsServerPanel";
import { RecordTable } from "@/features/dns/RecordTable";
import type { DnsOverview } from "@/features/dns/types";
import { ApiError } from "@/features/prepare/api";
import { HistoryTable } from "@/features/prepare/HistoryTable";
import type { OperationRow } from "@/features/prepare/types";

// Texto de un error de la API, o el de respaldo si es otro tipo de error.
function messageOf(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

// Pantalla del módulo "DNS": sin servidor, el formulario de crear (REQ-13); con servidor, su panel
// y la tabla de registros y resolvedores (REQ-17, REQ-23, REQ-31). Al final, el historial (REQ-36).
export function Dns() {
  // null hasta la primera lectura: así no aparece el formulario de crear antes de saber si hay servidor.
  const [overview, setOverview] = useState<DnsOverview | null>(null);
  const [listError, setListError] = useState("");
  const [history, setHistory] = useState<OperationRow[]>([]);
  const [historyError, setHistoryError] = useState("");
  // Verdadero mientras se recarga la pantalla tras una operación: desactiva los botones (REQ-38).
  const [reloading, setReloading] = useState(false);

  const loadOverview = useCallback(async () => {
    try {
      const data = await getDnsOverview();
      setOverview(data);
      setListError(data.list_error);
    } catch (error) {
      setListError(messageOf(error, "No se pudo cargar el estado del DNS."));
    }
  }, []);

  const loadHistory = useCallback(async () => {
    try {
      setHistory(await getDnsHistory());
      setHistoryError("");
    } catch (error) {
      setHistoryError(messageOf(error, "No se pudo cargar el historial."));
    }
  }, []);

  useEffect(() => {
    void loadOverview();
    void loadHistory();
  }, [loadOverview, loadHistory]);

  // Al terminar una operación se recargan el servidor, las VMs y el historial.
  const reload = useCallback(async () => {
    setReloading(true);
    try {
      await Promise.all([loadOverview(), loadHistory()]);
    } finally {
      setReloading(false);
    }
  }, [loadOverview, loadHistory]);

  const server = overview?.server ?? null;

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">DNS</h1>
        <p className="text-muted-foreground">
          Cree el servidor DNS de la red, registre los nombres de las VMs y úselo como su resolvedor.
        </p>
      </div>

      {listError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Estado de las VMs no disponible</AlertTitle>
          <AlertDescription>{listError}</AlertDescription>
        </Alert>
      )}

      {overview !== null && server === null && (
        <Card>
          <CardHeader>
            <CardTitle>Crear servidor DNS</CardTitle>
            <CardDescription>
              Crea la VM con bind9 en la IP, la zona y los reenviadores configurados en el backend, con los registros de
              los servidores web. Puede tardar varios minutos.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <CreateDnsForm busy={reloading} onDone={reload} />
          </CardContent>
        </Card>
      )}

      {server !== null && (
        <>
          <Card>
            <CardHeader>
              <CardTitle>Servidor DNS</CardTitle>
              <CardDescription>El único servidor DNS de la red y su servicio bind9.</CardDescription>
            </CardHeader>
            <CardContent>
              <DnsServerPanel server={server} busy={reloading} onDone={reload} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Registros y resolvedores</CardTitle>
              <CardDescription>
                Registros A de la zona {server.zone} y VMs que usan el servidor DNS como resolvedor.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <RecordTable vms={overview?.vms ?? []} busy={reloading} onDone={reload} />
            </CardContent>
          </Card>
        </>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Historial</CardTitle>
          <CardDescription>Últimas 50 operaciones del módulo, la más reciente primero.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {historyError && (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>Historial no disponible</AlertTitle>
              <AlertDescription>{historyError}</AlertDescription>
            </Alert>
          )}
          <HistoryTable operations={history} />
        </CardContent>
      </Card>
    </main>
  );
}
