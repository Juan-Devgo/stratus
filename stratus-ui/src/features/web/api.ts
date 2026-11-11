import { ApiError, errorMessage, request, type ActionResult } from "@/features/prepare/api";
import type { OperationRow } from "@/features/prepare/types";
import type { ServiceResult } from "@/features/services/types";

import type { CreateWebInput, UnitAction, WebServer } from "./types";

function serverPath(vm: string): string {
  return `/api/web/servers/${encodeURIComponent(vm)}`;
}

// Servidores web registrados con su estado leído de VirtualBox; list_error explica si no se pudo leer (REQ-6).
export function listWebServers(): Promise<{ servers: WebServer[]; list_error: string }> {
  return request("GET", "/api/web/servers");
}

// Crea el servidor web; warning avisa si no se pudo agregar su registro DNS (REQ-1, REQ-5).
export function createWeb(input: CreateWebInput): Promise<ActionResult> {
  return request("POST", "/api/web/servers", input);
}

// Elimina la VM del servidor web con su disco y su registro DNS (REQ-8).
export function deleteWeb(vm: string): Promise<ActionResult> {
  return request("DELETE", serverPath(vm));
}

// Sube el .tar.gz de la página como multipart con la única parte "archive" (REQ-9). El navegador
// pone el Content-Type con su separador.
export async function installPage(vm: string, archive: File): Promise<string> {
  const form = new FormData();
  form.append("archive", archive);
  let response: Response;
  try {
    response = await fetch(`${serverPath(vm)}/page`, { method: "POST", body: form });
  } catch {
    throw new ApiError(0, errorMessage(null));
  }
  const data: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    throw new ApiError(response.status, errorMessage(data));
  }
  return (data as { message: string }).message;
}

// Inicia, detiene o reinicia apache2 o nginx; start y restart devuelven además el estado leído (REQ-12).
export function webAction(vm: string, action: UnitAction): Promise<ServiceResult> {
  return request("POST", `${serverPath(vm)}/${action}`);
}

// Salida de "systemctl status" del servidor web; un estado failed se muestra, no falla (REQ-12).
export async function webStatus(vm: string): Promise<string> {
  const data = await request<{ output: string }>("GET", `${serverPath(vm)}/status`);
  return data.output;
}

// Últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
export async function getWebHistory(): Promise<OperationRow[]> {
  const data = await request<{ operations: OperationRow[] }>("GET", "/api/web/history");
  return data.operations;
}
