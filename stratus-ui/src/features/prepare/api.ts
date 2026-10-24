import type { OperationRow, Vm } from "./types";
import type { CreateVmInput, IssueInput } from "./validation";

// Error de la API con el texto "error" que envía el servidor, listo para mostrarse.
export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

// Mensaje cuando la respuesta no trae un "error" legible (por ejemplo, el backend está caído).
const GENERIC_ERROR = "No se pudo completar la operación. Revise que el backend esté en ejecución.";

// send hace la petición; si no hay conexión con el backend lanza ApiError.
export async function send(method: string, path: string, body?: unknown): Promise<Response> {
  const init: RequestInit = { method };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  try {
    return await fetch(path, init);
  } catch {
    throw new ApiError(0, GENERIC_ERROR);
  }
}

// errorMessage toma el texto "error" del cuerpo JSON, o el mensaje genérico si no lo hay.
export function errorMessage(data: unknown): string {
  return data && typeof data === "object" && "error" in data && typeof data.error === "string"
    ? data.error
    : GENERIC_ERROR;
}

// request hace la petición y devuelve el JSON; si el código no es 2xx lanza ApiError.
export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await send(method, path, body);
  const data: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    throw new ApiError(response.status, errorMessage(data));
  }
  return data as T;
}

// Las operaciones devuelven {"message": "..."}; aquí se entrega solo el texto.
export async function operation(method: string, path: string, body?: unknown): Promise<string> {
  const data = await request<{ message: string }>(method, path, body);
  return data.message;
}

function vmPath(name: string, action = ""): string {
  return `/api/prepare/vms/${encodeURIComponent(name)}${action}`;
}

export function listVms(): Promise<{ vms: Vm[]; list_error: string }> {
  return request("GET", "/api/prepare/vms");
}

export function createVm(input: CreateVmInput): Promise<string> {
  return operation("POST", "/api/prepare/vms", input);
}

// Encender una VM devuelve el mensaje y, si es un servidor o un balanceador y no se pudieron
// actualizar las IPs del balanceador, una advertencia (004, REQ-14).
export interface StartResult {
  message: string;
  warning?: string;
}

export function startVm(name: string): Promise<StartResult> {
  return request("POST", vmPath(name, "/start"));
}

export function stopVm(name: string): Promise<string> {
  return operation("POST", vmPath(name, "/stop"));
}

export function deleteVm(name: string): Promise<string> {
  return operation("DELETE", vmPath(name));
}

// Catálogo de paquetes sugeridos; la lista vive solo en el backend (REQ-5).
export async function getCatalog(): Promise<string[]> {
  const data = await request<{ packages: string[] }>("GET", "/api/prepare/catalog");
  return data.packages;
}

export function installPackage(name: string, pkg: string): Promise<string> {
  return operation("POST", vmPath(name, "/install-package"), { package: pkg });
}

export function removePackage(name: string, pkg: string): Promise<string> {
  return operation("POST", vmPath(name, "/remove-package"), { package: pkg });
}

// Genera la llave de root del usuario; la privada queda en el backend para una sola descarga.
export function generateUserKey(name: string): Promise<string> {
  return operation("POST", vmPath(name, "/user-key"));
}

// Descarga la llave privada pendiente. Si ya se descargó o venció, el backend responde 410 y
// se lanza ApiError con "La llave ya no está disponible, genere una nueva".
export async function downloadUserKey(name: string): Promise<Blob> {
  const response = await send("GET", vmPath(name, "/user-key/download"));
  if (!response.ok) {
    const data: unknown = await response.json().catch(() => null);
    throw new ApiError(response.status, errorMessage(data));
  }
  return response.blob();
}

// Reescribe /etc/issue con el mensaje y los campos; el backend los ordena según REQ-26.
export function setIssue(name: string, input: IssueInput): Promise<string> {
  return operation("POST", vmPath(name, "/issue"), input);
}

// Cambia el hostname de la VM; el script también actualiza la línea 127.0.1.1 de /etc/hosts.
export function setHostname(name: string, hostname: string): Promise<string> {
  return operation("POST", vmPath(name, "/hostname"), { hostname });
}

// Últimas 50 operaciones del módulo, la más reciente primero (REQ-18).
export async function getHistory(): Promise<OperationRow[]> {
  const data = await request<{ operations: OperationRow[] }>("GET", "/api/prepare/history");
  return data.operations;
}
