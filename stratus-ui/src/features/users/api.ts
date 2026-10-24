import { ApiError, errorMessage, operation, request, send } from "@/features/prepare/api";

import type { OperationRow } from "@/features/prepare/types";

import type { CreateUserInput, UserRow } from "./types";

function usersPath(vm: string): string {
  return `/api/users/vms/${encodeURIComponent(vm)}/users`;
}

// Usuarios regulares de la VM, leídos de la VM en cada llamada (REQ-1).
export async function listUsers(vm: string): Promise<UserRow[]> {
  const data = await request<{ users: UserRow[] }>("GET", usersPath(vm));
  return data.users;
}

// Crea el usuario; con ssh_key la llave privada queda en el backend para una sola descarga.
export function createUser(
  vm: string,
  input: CreateUserInput,
): Promise<{ message: string; key_available: boolean }> {
  return request("POST", usersPath(vm), input);
}

// Descarga la llave privada pendiente del usuario. Si ya se descargó o venció, el backend
// responde 410 y se lanza ApiError con "La llave ya no está disponible; ...".
export async function downloadUserKey(vm: string, username: string): Promise<Blob> {
  const response = await send("GET", `${usersPath(vm)}/${encodeURIComponent(username)}/key/download`);
  if (!response.ok) {
    const data: unknown = await response.json().catch(() => null);
    throw new ApiError(response.status, errorMessage(data));
  }
  return response.blob();
}

// Elimina el usuario; con removeFiles también borra todos sus archivos (REQ-13, REQ-16).
export function deleteUser(vm: string, username: string, removeFiles: boolean): Promise<string> {
  return operation("DELETE", `${usersPath(vm)}/${encodeURIComponent(username)}?remove_files=${removeFiles}`);
}

// Últimas 50 operaciones del módulo, la más reciente primero (REQ-20).
export async function getUsersHistory(): Promise<OperationRow[]> {
  const data = await request<{ operations: OperationRow[] }>("GET", "/api/users/history");
  return data.operations;
}
