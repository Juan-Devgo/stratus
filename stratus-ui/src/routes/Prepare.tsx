import { CircleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError, getCatalog, getHistory, listVms } from "@/features/prepare/api";
import { CreateVmForm } from "@/features/prepare/CreateVmForm";
import { HistoryTable } from "@/features/prepare/HistoryTable";
import { HostnameForm } from "@/features/prepare/HostnameForm";
import { IssueForm } from "@/features/prepare/IssueForm";
import { PackageForm } from "@/features/prepare/PackageForm";
import type { OperationRow, Vm } from "@/features/prepare/types";
import { UserKeyForm } from "@/features/prepare/UserKeyForm";
import { VmActions } from "@/features/prepare/VmActions";
import { VmSelector } from "@/features/prepare/VmSelector";

// Pantalla del módulo "Preparar sistema": la VM seleccionada es el objetivo de cada operación.
export function Prepare() {
  const [vms, setVms] = useState<Vm[]>([]);
  const [listError, setListError] = useState("");
  const [selectedVm, setSelectedVm] = useState<string | null>(null);
  const [catalog, setCatalog] = useState<string[]>([]);
  const [history, setHistory] = useState<OperationRow[]>([]);
  const [historyError, setHistoryError] = useState("");

  // Recarga la lista de VMs y el historial; cada formulario la llama al terminar (onDone)
  // para reflejar el nuevo estado y la operación recién registrada.
  const reload = useCallback(async () => {
    const loadVms = async () => {
      try {
        const data = await listVms();
        setVms(data.vms);
        setListError(data.list_error);
      } catch (error) {
        setListError(error instanceof ApiError ? error.message : "No se pudo cargar la lista de VMs.");
      }
    };
    const loadHistory = async () => {
      try {
        setHistory(await getHistory());
        setHistoryError("");
      } catch (error) {
        setHistoryError(error instanceof ApiError ? error.message : "No se pudo cargar el historial.");
      }
    };
    await Promise.all([loadVms(), loadHistory()]);
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  // El catálogo no cambia mientras corre el backend: se carga una vez. Si falla, el selector
  // queda vacío y el paquete aún se puede escribir a mano.
  useEffect(() => {
    getCatalog()
      .then(setCatalog)
      .catch(() => setCatalog([]));
  }, []);

  // Si la VM seleccionada se eliminó, deja de estar seleccionada.
  const vm = vms.find((item) => item.name === selectedVm) ?? null;

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">Preparar sistema</h1>
        <p className="text-muted-foreground">Cree máquinas virtuales Debian 13 y prepárelas para su uso.</p>
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
        <CardContent className="flex flex-col gap-4">
          <VmSelector vms={vms} value={vm?.name ?? null} onChange={setSelectedVm} />
          <VmActions vm={vm} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Aplicaciones</CardTitle>
          <CardDescription>Instale o elimine un paquete en la VM seleccionada.</CardDescription>
        </CardHeader>
        <CardContent>
          <PackageForm vm={vm} catalog={catalog} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Llave SSH de root</CardTitle>
          <CardDescription>
            Genere una llave ed25519 para entrar como root; el acceso con contraseña queda deshabilitado.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <UserKeyForm vm={vm} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Mensaje de inicio (/etc/issue)</CardTitle>
          <CardDescription>Componga el texto que se muestra antes del inicio de sesión en la consola.</CardDescription>
        </CardHeader>
        <CardContent>
          <IssueForm vm={vm} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Hostname</CardTitle>
          <CardDescription>Cambie el nombre de la VM seleccionada.</CardDescription>
        </CardHeader>
        <CardContent>
          <HostnameForm vm={vm} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Crear VM</CardTitle>
          <CardDescription>
            Crea un clon de Debian 13 sobre el disco base compartido (disco diferencial, red en puente, IP por DHCP) y
            lo prepara para el acceso por SSH. Puede tardar varios minutos.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <CreateVmForm onDone={reload} />
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
