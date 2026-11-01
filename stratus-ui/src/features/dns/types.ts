// Tipos de la API del módulo DNS (campos JSON en snake_case, como los envía el backend).

import type { VmKind, VmRole, VmState } from "@/features/prepare/types";

// El único servidor DNS (REQ-17).
export interface DnsServer {
  name: string;
  state: VmState;
  ip: string;
  zone: string; // dominio de la zona, por ejemplo stratus.lan
}

// Una VM de la tabla de registros (REQ-23, REQ-31); el servidor DNS no aparece.
export interface DnsVm {
  name: string;
  kind: Exclude<VmKind, "dns">;
  role: VmRole;
  state: VmState;
  ip: string; // IPv4 actual; "" si está apagada o no la reporta
  fqdn: string; // <vm>.<dominio> si tiene registro; si no, ""
  record_ip: string; // IPv4 guardada en su registro; si no tiene, ""
  resolver: boolean; // usa el servidor DNS como resolvedor
}

// Respuesta de GET /api/dns; server es null si no hay servidor DNS.
export interface DnsOverview {
  server: DnsServer | null;
  vms: DnsVm[];
  list_error: string;
}

// Resultado por VM de "Agregar registros" (REQ-23).
export interface RecordResult {
  vm: string;
  ok: boolean;
  message: string;
}
