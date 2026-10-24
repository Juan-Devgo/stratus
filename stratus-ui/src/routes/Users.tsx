import { CircleAlertIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError, listVms } from "@/features/prepare/api";
import { HistoryTable } from "@/features/prepare/HistoryTable";
import type { OperationRow, Vm } from "@/features/prepare/types";
import { VmSelector } from "@/features/prepare/VmSelector";
import { getUsersHistory, listUsers } from "@/features/users/api";
import { CreateUserForm } from "@/features/users/CreateUserForm";
import type { UserRow } from "@/features/users/types";
import { UserTable } from "@/features/users/UserTable";

// Pantalla del módulo "Gestión de usuarios": la VM seleccionada es el objetivo de cada operación.
export function Users() {
  const [vms, setVms] = useState<Vm[]>([]);
  const [listError, setListError] = useState("");
  const [selectedVm, setSelectedVm] = useState<string | null>(null);
  const [users, setUsers] = useState<UserRow[]>([]);
  const [usersError, setUsersError] = useState("");
  const [history, setHistory] = useState<OperationRow[]>([]);
  const [historyError, setHistoryError] = useState("");
  // Número de la última carga de usuarios: una respuesta tardía de otra VM no pisa la actual.
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

  // Lee los usuarios de la VM seleccionada. Sin VM la lista queda vacía y no se pide nada (AC-2).
  const loadUsers = useCallback(async () => {
    const id = ++loadId.current;
    if (vmName === null) {
      setUsers([]);
      setUsersError("");
      return;
    }
    try {
      const rows = await listUsers(vmName);
      if (id !== loadId.current) {
        return;
      }
      setUsers(rows);
      setUsersError("");
      toast.success("Lista de usuarios actualizada");
    } catch (error) {
      if (id !== loadId.current) {
        return;
      }
      setUsers([]);
      setUsersError(error instanceof ApiError ? error.message : "No se pudo cargar la lista de usuarios.");
    }
  }, [vmName]);

  useEffect(() => {
    void loadUsers();
  }, [loadUsers]);

  // Últimas 50 operaciones del módulo (REQ-20).
  const loadHistory = useCallback(async () => {
    try {
      setHistory(await getUsersHistory());
      setHistoryError("");
    } catch (error) {
      setHistoryError(error instanceof ApiError ? error.message : "No se pudo cargar el historial.");
    }
  }, []);

  useEffect(() => {
    void loadHistory();
  }, [loadHistory]);

  // Al terminar una operación se vuelven a leer los usuarios (con su aviso) y el historial.
  const reload = useCallback(async () => {
    await Promise.all([loadUsers(), loadHistory()]);
  }, [loadUsers, loadHistory]);

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">Gestión de usuarios</h1>
        <p className="text-muted-foreground">Cree y elimine usuarios de la VM, con sudo y llaves SSH opcionales.</p>
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
          <CardTitle>Usuarios</CardTitle>
          <CardDescription>Usuarios regulares de la VM seleccionada, leídos de la VM.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {usersError && (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>Usuarios no disponibles</AlertTitle>
              <AlertDescription>{usersError}</AlertDescription>
            </Alert>
          )}
          <UserTable users={users} vm={vm} onDone={reload} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Crear usuario</CardTitle>
          <CardDescription>
            Cree un usuario en la VM seleccionada, con sudo sin contraseña y llaves SSH opcionales.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {/* Al terminar se vuelve a leer la lista, que muestra el aviso (REQ-18), y el historial. */}
          <CreateUserForm vm={vm} onDone={reload} />
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
