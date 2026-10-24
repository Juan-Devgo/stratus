import { CircleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { AssignServerForm } from "@/features/haproxy/AssignServerForm";
import { getHaproxyHistory, getStatus, listLbs, listServers } from "@/features/haproxy/api";
import { CreateLbForm } from "@/features/haproxy/CreateLbForm";
import { FrontendForm } from "@/features/haproxy/FrontendForm";
import { LbActions } from "@/features/haproxy/LbActions";
import { LbTable } from "@/features/haproxy/LbTable";
import { ServerTable } from "@/features/haproxy/ServerTable";
import type { Lb, LbServer } from "@/features/haproxy/types";
import { ApiError, listVms } from "@/features/prepare/api";
import { HistoryTable } from "@/features/prepare/HistoryTable";
import type { OperationRow, Vm } from "@/features/prepare/types";

// Texto de un error de la API, o el de respaldo si es otro tipo de error.
function messageOf(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

// Pantalla del módulo "Backends HAProxy": lista de balanceadores y, para el seleccionado, sus
// acciones, el puerto del frontend, sus servidores y el historial del módulo.
export function HAProxy() {
  const [lbs, setLbs] = useState<Lb[]>([]);
  const [listError, setListError] = useState("");
  const [vms, setVms] = useState<Vm[]>([]);
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [servers, setServers] = useState<LbServer[]>([]);
  const [serversError, setServersError] = useState("");
  const [statusError, setStatusError] = useState("");
  const [history, setHistory] = useState<OperationRow[]>([]);
  const [historyError, setHistoryError] = useState("");
  // Verdadero mientras se recarga la pantalla tras una operación: desactiva el formulario de crear.
  const [reloading, setReloading] = useState(false);
  // Número de la última carga de servidores: una respuesta tardía de otro balanceador no pisa la actual.
  const loadId = useRef(0);
  // Última lista de balanceadores, para que loadServers lea el estado recién cargado.
  const lbsRef = useRef<Lb[]>([]);

  const selected = lbs.find((item) => item.name === selectedName) ?? null;
  const selectedKey = selected?.name ?? null;
  const selectedState = selected?.state ?? null;

  const loadLbs = useCallback(async (): Promise<Lb[]> => {
    try {
      const data = await listLbs();
      lbsRef.current = data.lbs;
      setLbs(data.lbs);
      setListError(data.list_error);
      return data.lbs;
    } catch (error) {
      setListError(messageOf(error, "No se pudo cargar la lista de balanceadores."));
      return lbsRef.current;
    }
  }, []);

  const loadVms = useCallback(async () => {
    try {
      setVms((await listVms()).vms);
    } catch {
      // El selector de VMs del formulario de asignar queda con la última lista conocida.
    }
  }, []);

  const loadHistory = useCallback(async () => {
    try {
      setHistory(await getHaproxyHistory());
      setHistoryError("");
    } catch (error) {
      setHistoryError(messageOf(error, "No se pudo cargar el historial."));
    }
  }, []);

  // Lee los servidores guardados del balanceador seleccionado. Si está encendido pide además el
  // estado a HAProxy y avisa con un toast; si está apagado no hace esa llamada y el estado queda "-"
  // (REQ-20). Sin balanceador seleccionado no pide nada.
  const loadServers = useCallback(async () => {
    const id = ++loadId.current;
    const lb = lbsRef.current.find((item) => item.name === selectedKey) ?? null;
    if (lb === null) {
      setServers([]);
      setServersError("");
      setStatusError("");
      return;
    }
    let rows: LbServer[];
    try {
      rows = await listServers(lb.name);
    } catch (error) {
      if (id === loadId.current) {
        setServers([]);
        setServersError(messageOf(error, "No se pudo cargar la lista de servidores."));
      }
      return;
    }
    if (id !== loadId.current) {
      return;
    }
    setServers(rows);
    setServersError("");
    if (lb.state !== "running") {
      setStatusError("");
      return;
    }
    try {
      const states = await getStatus(lb.name);
      if (id !== loadId.current) {
        return;
      }
      setServers(rows.map((row) => ({ ...row, state: states[row.name] ?? "-" })));
      setStatusError("");
      toast.success("Estado del balanceador actualizado");
    } catch (error) {
      if (id === loadId.current) {
        setStatusError(messageOf(error, "No se pudo leer el estado del balanceador."));
      }
    }
  }, [selectedKey]);

  useEffect(() => {
    void loadLbs();
    void loadVms();
    void loadHistory();
  }, [loadLbs, loadVms, loadHistory]);

  // Al seleccionar otro balanceador, o cuando cambia su estado, se vuelven a leer sus servidores.
  useEffect(() => {
    void loadServers();
  }, [loadServers, selectedState]);

  // Al terminar una operación se recargan balanceadores, VMs, historial y servidores.
  const reload = useCallback(async () => {
    setReloading(true);
    try {
      await Promise.all([loadLbs(), loadVms(), loadHistory()]);
      await loadServers();
    } finally {
      setReloading(false);
    }
  }, [loadLbs, loadVms, loadHistory, loadServers]);

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">Backends HAProxy</h1>
        <p className="text-muted-foreground">
          Cree balanceadores HAProxy, asigne servidores y vea su estado.
        </p>
      </div>

      {listError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Estado de los balanceadores no disponible</AlertTitle>
          <AlertDescription>{listError}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Balanceadores</CardTitle>
          <CardDescription>Seleccione el balanceador sobre el que se aplican las operaciones.</CardDescription>
        </CardHeader>
        <CardContent>
          <LbTable lbs={lbs} selected={selectedKey} onSelect={setSelectedName} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Crear balanceador</CardTitle>
          <CardDescription>
            Crea una VM con HAProxy, su red interna y su servidor DHCP. Puede tardar varios minutos.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <CreateLbForm busy={reloading} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Acciones</CardTitle>
          <CardDescription>Actualizar las IPs internas de los servidores o eliminar el balanceador.</CardDescription>
        </CardHeader>
        <CardContent>
          <LbActions lb={selected} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Frontend</CardTitle>
          <CardDescription>Puerto en el que el balanceador recibe las peticiones.</CardDescription>
        </CardHeader>
        <CardContent>
          <FrontendForm lb={selected} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Servidores</CardTitle>
          <CardDescription>Servidores del balanceador seleccionado y su estado en HAProxy.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {selected === null ? (
            <p className="text-sm text-muted-foreground">Seleccione un balanceador para continuar</p>
          ) : (
            <>
              {serversError && (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>Servidores no disponibles</AlertTitle>
                  <AlertDescription>{serversError}</AlertDescription>
                </Alert>
              )}
              {statusError && (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>Estado no disponible</AlertTitle>
                  <AlertDescription>{statusError}</AlertDescription>
                </Alert>
              )}
              <ServerTable servers={servers} lbOff={selected.state !== "running"} lb={selected.name} onDone={reload} />
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Asignar servidor</CardTitle>
          <CardDescription>
            Convierte una VM común en servidor del balanceador seleccionado. La VM se reinicia.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <AssignServerForm lb={selected} vms={vms} onDone={reload} />
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
