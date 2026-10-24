// Tipos de la API del módulo prepare (campos JSON en snake_case, como los envía el backend).

export type VmState = "running" | "poweroff" | "unknown";

// Rol de la VM según la topología de HAProxy (004): común, balanceador o servidor de un balanceador.
export type VmRole = "vm" | "lb" | "server";

export interface Vm {
  name: string;
  state: VmState;
  ip: string; // IPv4 de la VM; "" si está apagada o no la reporta (de un servidor, su IP interna)
  user_key_generated: boolean;
  role: VmRole;
  lb: string; // de un servidor, el nombre de su balanceador; si no, ""
  lb_ip: string; // de un servidor, la IPv4 en puente de su balanceador; si no, ""
}

export interface OperationRow {
  id: number;
  operation: string;
  vm: string;
  command: string;
  args: string[];
  exit_code: number;
  stderr: string;
  result: "ok" | "error" | "timeout";
  created_at: string;
}

// Claves de los campos dinámicos de /etc/issue (REQ-10), las mismas que acepta el backend.
export type IssueField = "hostname" | "date" | "time" | "ipv4" | "os" | "kernel" | "arch" | "tty" | "users";
