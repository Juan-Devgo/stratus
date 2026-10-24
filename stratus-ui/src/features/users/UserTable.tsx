import { Trash2Icon, UsersIcon } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { Vm } from "@/features/prepare/types";

import { DeleteUserDialog } from "./DeleteUserDialog";
import type { UserRow } from "./types";

type Props = { users: UserRow[]; vm: Vm | null; onDone(): void };

const yesNo = (value: boolean) => (value ? "Sí" : "No");

// Usuarios regulares de la VM con su estado de sudo y de llave SSH (REQ-1). Cada fila tiene un
// botón que abre la confirmación de borrado (REQ-13).
export function UserTable({ users, vm, onDone }: Props) {
  // Usuario cuyo diálogo de borrado está abierto, o null.
  const [deleting, setDeleting] = useState<string | null>(null);

  return (
    <>
      {users.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <UsersIcon />
            </EmptyMedia>
            <EmptyTitle>Sin usuarios regulares</EmptyTitle>
            <EmptyDescription>Seleccione una VM para ver sus usuarios (UID 1000-59999).</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Usuario</TableHead>
              <TableHead>UID</TableHead>
              <TableHead>Sudo</TableHead>
              <TableHead>Llave</TableHead>
              <TableHead>Acciones</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((user) => (
              <TableRow key={user.username}>
                <TableCell>{user.username}</TableCell>
                <TableCell>{user.uid}</TableCell>
                <TableCell>{yesNo(user.sudo)}</TableCell>
                <TableCell>{yesNo(user.ssh_key)}</TableCell>
                <TableCell>
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={`Eliminar ${user.username}`}
                    disabled={vm === null}
                    onClick={() => setDeleting(user.username)}
                  >
                    <Trash2Icon data-icon="inline-start" />
                    Eliminar
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {/* Fuera de la tabla: el diálogo sigue abierto aunque la lista se recargue o quede vacía. */}
      <DeleteUserDialog vm={vm?.name ?? ""} username={deleting} onClose={() => setDeleting(null)} onDone={onDone} />
    </>
  );
}
