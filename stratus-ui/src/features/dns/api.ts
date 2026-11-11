import { operation, request } from "@/features/prepare/api";
import type { OperationRow } from "@/features/prepare/types";
import type { CreateVmInput } from "@/features/prepare/validation";
import type { ServiceResult } from "@/features/services/types";
import type { UnitAction } from "@/features/web/types";

import type { DnsOverview, RecordResult } from "./types";

// Servidor DNS y demás VMs con su registro (REQ-17, REQ-23).
export function getDnsOverview(): Promise<DnsOverview> {
  return request("GET", "/api/dns");
}

// Crea el único servidor DNS con la configuración APP_DNS_* del backend (REQ-13).
export function createDns(input: CreateVmInput): Promise<string> {
  return operation("POST", "/api/dns/server", input);
}

// Elimina el servidor DNS con su disco y todos los registros (REQ-21).
export function deleteDns(): Promise<string> {
  return operation("DELETE", "/api/dns/server");
}

// Inicia, detiene o reinicia bind9; start y restart devuelven además el estado leído (REQ-12).
export function dnsAction(action: UnitAction): Promise<ServiceResult> {
  return request("POST", `/api/dns/server/${action}`);
}

// Salida de "systemctl status named" (REQ-12).
export async function dnsStatus(): Promise<string> {
  const data = await request<{ output: string }>("GET", "/api/dns/server/status");
  return data.output;
}

// Agrega los registros A de las VMs elegidas; results trae el resultado de cada una (REQ-23).
export function addRecords(vms: string[]): Promise<{ message: string; results: RecordResult[] }> {
  return request("POST", "/api/dns/records", { vms });
}

// Quita el registro A de la VM (REQ-24).
export function removeRecord(vm: string): Promise<string> {
  return operation("DELETE", `/api/dns/records/${encodeURIComponent(vm)}`);
}

// Actualiza las IPs de todos los registros; la zona solo se recarga si cambió (REQ-27).
export function refreshRecords(): Promise<string> {
  return operation("POST", "/api/dns/records/refresh");
}

// Últimas 50 operaciones del módulo, la más reciente primero (REQ-36).
export async function getDnsHistory(): Promise<OperationRow[]> {
  const data = await request<{ operations: OperationRow[] }>("GET", "/api/dns/history");
  return data.operations;
}
