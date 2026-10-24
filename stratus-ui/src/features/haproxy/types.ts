// Tipos de la API del módulo HAProxy (campos JSON en snake_case, como los envía el backend).

import type { VmState } from "@/features/prepare/types";

export interface Lb {
  name: string;
  state: VmState;
  ip: string; // IPv4 en puente; "" si está apagado o no la reporta
  subnet: string; // "10.10.N.0/24"
  frontend_port: number;
  server_count: number;
}

export interface LbServer {
  name: string; // server<N>
  vm: string;
  ip: string; // última IP interna conocida
  port: number;
  state: string; // UP, DOWN, MAINT... o "-" si no se leyó
}
