import { CircleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError } from "@/features/prepare/api";
import { HistoryTable } from "@/features/prepare/HistoryTable";
import type { OperationRow } from "@/features/prepare/types";
import { getWebHistory, listWebServers } from "@/features/web/api";
import { CreateWebForm } from "@/features/web/CreateWebForm";
import type { WebServer } from "@/features/web/types";
import { WebTable } from "@/features/web/WebTable";

// Texto de un error de la API, o el de respaldo si es otro tipo de error.
function messageOf(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

// Pantalla del módulo "Servidores web": lista de servidores Apache y Nginx con sus acciones, el
// formulario de crear y el historial del módulo (REQ-6, REQ-12, REQ-36).
export function Web() {
  const [servers, setServers] = useState<WebServer[]>([]);
  const [listError, setListError] = useState("");
  const [history, setHistory] = useState<OperationRow[]>([]);
  const [historyError, setHistoryError] = useState("");
  // Verdadero mientras se recarga la pantalla tras una operación: desactiva los botones (REQ-38).
  const [reloading, setReloading] = useState(false);

  const loadServers = useCallback(async () => {
    try {
      const data = await listWebServers();
      setServers(data.servers);
      setListError(data.list_error);
    } catch (error) {
      setListError(messageOf(error, "No se pudo cargar la lista de servidores web."));
    }
  }, []);

  const loadHistory = useCallback(async () => {
    try {
      setHistory(await getWebHistory());
      setHistoryError("");
    } catch (error) {
      setHistoryError(messageOf(error, "No se pudo cargar el historial."));
    }
  }, []);

  useEffect(() => {
    void loadServers();
    void loadHistory();
  }, [loadServers, loadHistory]);

  // Al terminar una operación se recargan la lista y el historial.
  const reload = useCallback(async () => {
    setReloading(true);
    try {
      await Promise.all([loadServers(), loadHistory()]);
    } finally {
      setReloading(false);
    }
  }, [loadServers, loadHistory]);

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">Servidores web</h1>
        <p className="text-muted-foreground">Cree servidores Apache o Nginx, instale su página y controle el servicio.</p>
      </div>

      {listError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Estado de los servidores no disponible</AlertTitle>
          <AlertDescription>{listError}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Servidores</CardTitle>
          <CardDescription>Servidores web creados por la consola y su estado en VirtualBox.</CardDescription>
        </CardHeader>
        <CardContent>
          <WebTable servers={servers} busy={reloading} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Crear servidor web</CardTitle>
          <CardDescription>
            Crea una VM desde el disco base de Apache o de Nginx y, si hay servidor DNS, su registro. Puede tardar
            varios minutos.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <CreateWebForm busy={reloading} onDone={reload} />
        </CardContent>
      </Card>

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
