import { CircleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { OperationResult } from "@/components/OperationResult";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { ApiError, listVms } from "@/features/prepare/api";
import { HistoryTable } from "@/features/prepare/HistoryTable";
import type { OperationRow, Vm } from "@/features/prepare/types";
import { useOperation } from "@/features/prepare/useOperation";
import { VmSelector } from "@/features/prepare/VmSelector";
import { getServiceLogs, getServiceStatus, getServicesHistory, listServices, runServiceAction } from "@/features/services/api";
import { CreateServiceForm } from "@/features/services/CreateServiceForm";
import { DeleteServiceDialog } from "@/features/services/DeleteServiceDialog";
import { EditServiceDialog } from "@/features/services/EditServiceDialog";
import { RedeployServiceDialog } from "@/features/services/RedeployServiceDialog";
import { ServiceOutputPanel } from "@/features/services/ServiceOutputPanel";
import { ServiceTable } from "@/features/services/ServiceTable";
import { StateWarning } from "@/features/services/StateWarning";
import type { ServiceAction, ServiceRow } from "@/features/services/types";

// Pantalla del módulo "Servicios systemd": la VM seleccionada es el objetivo de cada operación.
export function Services() {
  const [vms, setVms] = useState<Vm[]>([]);
  const [listError, setListError] = useState("");
  const [selectedVm, setSelectedVm] = useState<string | null>(null);
  const [services, setServices] = useState<ServiceRow[]>([]);
  const [servicesError, setServicesError] = useState("");
  const [history, setHistory] = useState<OperationRow[]>([]);
  const [historyError, setHistoryError] = useState("");
  // El formulario de creación avisa cuando tiene una operación en curso (REQ-29).
  const [creating, setCreating] = useState(false);
  // Acciones de las filas (iniciar, detener...): una sola operación a la vez.
  const action = useOperation();
  // Servicio y estado del último resultado de una acción, para el aviso de estado (REQ-15).
  const [acted, setActed] = useState<{ name: string; state?: string }>({ name: "" });
  // Lectura de Estado o Logs en curso, su salida para el panel "Salida" y su error, si falló.
  const [reading, setReading] = useState(false);
  const [output, setOutput] = useState<{ title: string; text: string } | null>(null);
  const [readError, setReadError] = useState("");
  // Servicio cuyo diálogo de edición está abierto, o null.
  const [editing, setEditing] = useState<ServiceRow | null>(null);
  // Servicio cuyo diálogo de redespliegue está abierto, o null.
  const [redeploying, setRedeploying] = useState<ServiceRow | null>(null);
  // Servicio cuyo diálogo de borrado está abierto, o null.
  const [deleting, setDeleting] = useState<string | null>(null);
  // Cualquier operación en curso deshabilita los botones de la pantalla.
  const busy = creating || action.pending || reading;
  // Número de la última carga de servicios: una respuesta tardía de otra VM no pisa la actual.
  const loadId = useRef(0);

  useEffect(() => {
    listVms()
      .then((data) => {
        setVms(data.vms);
        setListError(data.list_error);
      })
      .catch((error) => {
        setListError(error instanceof ApiError ? error.message : "No se pudo cargar la lista de VMs.");
      });
  }, []);

  // Si la VM seleccionada ya no existe en la lista, deja de estar seleccionada.
  const vm = vms.find((item) => item.name === selectedVm) ?? null;
  const vmName = vm?.name ?? null;

  // Lee los servicios de la VM seleccionada. Sin VM la lista queda vacía y no se pide nada (AC-16).
  const loadServices = useCallback(async () => {
    const id = ++loadId.current;
    if (vmName === null) {
      setServices([]);
      setServicesError("");
      return;
    }
    try {
      const rows = await listServices(vmName);
      if (id !== loadId.current) {
        return;
      }
      setServices(rows);
      setServicesError("");
      toast.success("Lista de servicios actualizada");
    } catch (error) {
      if (id !== loadId.current) {
        return;
      }
      setServices([]);
      setServicesError(error instanceof ApiError ? error.message : "No se pudo cargar la lista de servicios.");
    }
  }, [vmName]);

  useEffect(() => {
    void loadServices();
  }, [loadServices]);

  // Últimas 50 operaciones del módulo (REQ-28).
  const loadHistory = useCallback(async () => {
    try {
      setHistory(await getServicesHistory());
      setHistoryError("");
    } catch (error) {
      setHistoryError(error instanceof ApiError ? error.message : "No se pudo cargar el historial.");
    }
  }, []);

  useEffect(() => {
    void loadHistory();
  }, [loadHistory]);

  // Al terminar una operación se vuelven a leer los servicios (con su aviso) y el historial.
  const reload = useCallback(async () => {
    await Promise.all([loadServices(), loadHistory()]);
  }, [loadServices, loadHistory]);

  // La salida de Estado o Logs y el diálogo de edición son de una VM concreta: al cambiar de VM se descartan.
  useEffect(() => {
    setOutput(null);
    setReadError("");
    setEditing(null);
    setRedeploying(null);
    setDeleting(null);
  }, [vmName]);

  // Ejecuta una acción de systemctl sobre un servicio y vuelve a leer la lista (con su aviso).
  async function handleAction(name: string, serviceAction: ServiceAction) {
    if (vmName === null) {
      return;
    }
    setActed({ name });
    await action.run(async () => {
      const done = await runServiceAction(vmName, name, serviceAction);
      setActed({ name, state: done.active_state });
      return done.message;
    });
    await reload();
  }

  // Estado y Logs no cambian nada: su salida va al panel y un servicio fallido no es un error.
  async function handleRead(name: string, kind: "Estado" | "Logs") {
    if (vmName === null) {
      return;
    }
    setReading(true);
    setReadError("");
    try {
      const text = kind === "Estado" ? await getServiceStatus(vmName, name) : await getServiceLogs(vmName, name);
      setOutput({ title: `${kind} de ${name}`, text });
    } catch (error) {
      setOutput(null);
      setReadError(error instanceof ApiError ? error.message : "Ocurrió un error inesperado en la consola.");
      // Una lectura fallida queda en el historial (REQ-27).
      void loadHistory();
    } finally {
      setReading(false);
    }
  }

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">Servicios systemd</h1>
        <p className="text-muted-foreground">
          Despliegue aplicaciones como servicios systemd en la VM y contrólelos desde la consola.
        </p>
      </div>

      {listError && (
        <Alert variant="destructive">
          <CircleAlertIcon />
          <AlertTitle>Estado de las VMs no disponible</AlertTitle>
          <AlertDescription>{listError}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Máquina virtual</CardTitle>
          <CardDescription>Seleccione la VM sobre la que se aplican las operaciones.</CardDescription>
        </CardHeader>
        <CardContent>
          <VmSelector vms={vms} value={vmName} onChange={setSelectedVm} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Servicios</CardTitle>
          <CardDescription>Servicios creados por Stratus en la VM seleccionada, leídos de la VM.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {servicesError && (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>Servicios no disponibles</AlertTitle>
              <AlertDescription>{servicesError}</AlertDescription>
            </Alert>
          )}
          {(action.pending || reading) && <Spinner aria-label="Cargando" />}
          <OperationResult result={action.result} />
          {action.result?.ok && <StateWarning name={acted.name} state={acted.state} />}
          {readError && <OperationResult result={{ ok: false, text: readError }} />}
          <ServiceTable
            services={services}
            busy={busy}
            onAction={handleAction}
            onStatus={(name) => void handleRead(name, "Estado")}
            onLogs={(name) => void handleRead(name, "Logs")}
            onEdit={setEditing}
            onRedeploy={setRedeploying}
            onDelete={setDeleting}
          />
        </CardContent>
      </Card>

      {/* Fuera de la tabla: el diálogo sigue abierto aunque la lista se recargue. */}
      <EditServiceDialog vm={vmName ?? ""} service={editing} onClose={() => setEditing(null)} onDone={reload} />
      <RedeployServiceDialog
        vm={vmName ?? ""}
        service={redeploying}
        onClose={() => setRedeploying(null)}
        onDone={reload}
      />

      <DeleteServiceDialog vm={vmName ?? ""} name={deleting} onClose={() => setDeleting(null)} onDone={reload} />

      {output && <ServiceOutputPanel title={output.title} output={output.text} />}

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

      <Card>
        <CardHeader>
          <CardTitle>Crear servicio</CardTitle>
          <CardDescription>
            Despliegue un archivo .tar.gz como servicio systemd con su propio usuario, archivo de variables y unidad.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {/* Al terminar se vuelve a leer la lista, que muestra el aviso (REQ-16). */}
          <CreateServiceForm vm={vm} busy={busy} onBusyChange={setCreating} onDone={reload} />
        </CardContent>
      </Card>
    </main>
  );
}
