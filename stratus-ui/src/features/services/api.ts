import { ApiError, errorMessage, operation, request } from "@/features/prepare/api";

import type { OperationRow } from "@/features/prepare/types";

import type { CreateServiceInput, EditServiceInput, ServiceAction, ServiceResult, ServiceRow } from "./types";

function servicesPath(vm: string): string {
  return `/api/services/vms/${encodeURIComponent(vm)}/services`;
}

// Servicios de Stratus de la VM, leídos de la VM en cada llamada (REQ-16).
export async function listServices(vm: string): Promise<ServiceRow[]> {
  const data = await request<{ services: ServiceRow[] }>("GET", servicesPath(vm));
  return data.services;
}

// sendForm envía el JSON "payload" y el archivo como multipart/form-data, en ese orden: el backend
// exige payload primero (REQ-4). El navegador pone el Content-Type con su separador.
async function sendForm<T>(method: string, path: string, payload: unknown, archive: File): Promise<T> {
  const form = new FormData();
  form.append("payload", JSON.stringify(payload));
  form.append("archive", archive);
  let response: Response;
  try {
    response = await fetch(path, { method, body: form });
  } catch {
    throw new ApiError(0, errorMessage(null));
  }
  const data: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    throw new ApiError(response.status, errorMessage(data));
  }
  return data as T;
}

// Crea el servicio: sube el .tar.gz y las variables; el backend devuelve el estado leído tras iniciar.
export function createService(vm: string, input: CreateServiceInput, archive: File): Promise<ServiceResult> {
  return sendForm("POST", servicesPath(vm), input, archive);
}

// Ejecuta start, stop, restart, enable o disable; start y restart devuelven además el estado leído (REQ-17).
export function runServiceAction(vm: string, name: string, action: ServiceAction): Promise<ServiceResult> {
  return request("POST", `${servicesPath(vm)}/${encodeURIComponent(name)}/${action}`);
}

// Salida de "systemctl status" del servicio; un servicio inactivo o fallido se muestra, no falla (REQ-19).
export async function getServiceStatus(vm: string, name: string): Promise<string> {
  const data = await request<{ output: string }>("GET", `${servicesPath(vm)}/${encodeURIComponent(name)}/status`);
  return data.output;
}

// Últimas 100 líneas del diario del servicio, sin seguimiento en vivo (REQ-20).
export async function getServiceLogs(vm: string, name: string): Promise<string> {
  const data = await request<{ output: string }>("GET", `${servicesPath(vm)}/${encodeURIComponent(name)}/logs`);
  return data.output;
}

// Edita la unidad y, con replace_env, el archivo de variables completo (REQ-22, REQ-23).
export function editService(vm: string, name: string, input: EditServiceInput): Promise<ServiceResult> {
  return request("PUT", `${servicesPath(vm)}/${encodeURIComponent(name)}`, input);
}

// Reemplaza el contenido de /opt/<name> con un .tar.gz nuevo; el ejecutable puede cambiar (REQ-24).
export function redeployService(vm: string, name: string, executable: string, archive: File): Promise<ServiceResult> {
  return sendForm("POST", `${servicesPath(vm)}/${encodeURIComponent(name)}/redeploy`, { executable }, archive);
}

// Elimina el servicio con todo lo que instaló; devuelve el mensaje del backend (REQ-25).
export function deleteService(vm: string, name: string): Promise<string> {
  return operation("DELETE", `${servicesPath(vm)}/${encodeURIComponent(name)}`);
}

// Últimas 50 operaciones del módulo, la más reciente primero (REQ-28).
export async function getServicesHistory(): Promise<OperationRow[]> {
  const data = await request<{ operations: OperationRow[] }>("GET", "/api/services/history");
  return data.operations;
}
