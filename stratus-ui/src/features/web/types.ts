// Tipos de la API del módulo de servidores web (campos JSON en snake_case, como los envía el backend).

import type { VmState } from "@/features/prepare/types";
import type { CreateVmInput } from "@/features/prepare/validation";

export type WebType = "apache" | "nginx";

// Las tres acciones de systemctl sobre apache2 o nginx (REQ-12).
export type UnitAction = "start" | "stop" | "restart";

// Un servidor web tal como lo lista GET /api/web/servers (REQ-6).
export interface WebServer {
  name: string;
  type: WebType;
  state: VmState;
  ip: string; // IPv4 actual; "" si está apagado o no la reporta
  fqdn: string; // <vm>.<dominio> si tiene registro DNS; si no, ""
  lb: string; // balanceador al que está asignado; si no, ""
}

// Cuerpo de POST /api/web/servers: los campos de 001 más el tipo (REQ-1). type es "" mientras no
// se elige, y así lo rechaza la validación.
export interface CreateWebInput extends CreateVmInput {
  type: WebType | "";
}
