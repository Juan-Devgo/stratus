import { operation, request } from "@/features/prepare/api";
import type { OperationRow } from "@/features/prepare/types";
import type { CreateVmInput } from "@/features/prepare/validation";

import type { Lb, LbServer } from "./types";

function lbPath(lb: string, action = ""): string {
  return `/api/haproxy/lbs/${encodeURIComponent(lb)}${action}`;
}

// Balanceadores con su estado; list_error explica por qué algún estado quedó "unknown".
export function listLbs(): Promise<{ lbs: Lb[]; list_error: string }> {
  return request("GET", "/api/haproxy/lbs");
}

// Crea el balanceador con las mismas reglas de nombre, memoria y CPUs que una VM (REQ-1).
export function createLb(input: CreateVmInput): Promise<string> {
  return operation("POST", "/api/haproxy/lbs", input);
}

export function deleteLb(lb: string): Promise<string> {
  return operation("DELETE", lbPath(lb));
}

export function saveFrontend(lb: string, port: number): Promise<string> {
  return operation("PUT", lbPath(lb, "/frontend"), { port });
}

// Vuelve a leer las IPs internas de los servidores y reescribe el balanceador (REQ-14).
export function refreshLb(lb: string): Promise<string> {
  return operation("POST", lbPath(lb, "/refresh"));
}

// Servidores guardados del balanceador, con estado "-": no se consulta a HAProxy.
export async function listServers(lb: string): Promise<LbServer[]> {
  const data = await request<{ servers: LbServer[] }>("GET", lbPath(lb, "/servers"));
  return data.servers;
}

// Estado de cada servidor leído de HAProxy; solo con el balanceador encendido (REQ-20).
export async function getStatus(lb: string): Promise<Record<string, string>> {
  const data = await request<{ states: Record<string, string> }>("GET", lbPath(lb, "/status"));
  return data.states;
}

export function assignServer(lb: string, vm: string, port: number): Promise<string> {
  return operation("POST", lbPath(lb, "/servers"), { vm, port });
}

export function removeServer(lb: string, vm: string): Promise<string> {
  return operation("DELETE", lbPath(lb, `/servers/${encodeURIComponent(vm)}`));
}

// Últimas 50 operaciones del módulo, la más reciente primero (REQ-24).
export async function getHaproxyHistory(): Promise<OperationRow[]> {
  const data = await request<{ operations: OperationRow[] }>("GET", "/api/haproxy/history");
  return data.operations;
}
