// Tipos de la API del módulo de servicios systemd (campos JSON en snake_case, como los envía el backend).

export type Restart = "no" | "always" | "on-failure";

// Las cinco acciones de systemctl que tiene cada fila (REQ-17).
export type ServiceAction = "start" | "stop" | "restart" | "enable" | "disable";

// Un servicio creado por la app, tal como lo lista la VM (REQ-16). env_keys solo trae las claves:
// los valores nunca salen de la VM (REQ-13).
export interface ServiceRow {
  name: string;
  description: string;
  active_state: string;
  enabled_state: string;
  restart: Restart;
  restart_sec: number;
  executable: string;
  args: string;
  env_keys: string[];
}

// Una variable de entorno; value es un secreto.
export interface EnvVar {
  key: string;
  value: string;
}

// Los campos de la unidad que se pueden editar (REQ-11).
export interface ServiceConfig {
  description: string;
  executable: string;
  args: string;
  restart: Restart;
  restart_sec: number;
}

// Parte "payload" de POST /api/services/vms/{vm}/services (el archivo va aparte).
export interface CreateServiceInput extends ServiceConfig {
  name: string;
  env: EnvVar[];
}

// Cuerpo de PUT /api/services/vms/{vm}/services/{name}. Con replace_env=false, env va vacío.
export interface EditServiceInput extends ServiceConfig {
  replace_env: boolean;
  env: EnvVar[];
}

// Respuesta de las operaciones que cambian un servicio; active_state solo viene tras iniciar,
// reiniciar, crear, editar o redesplegar (REQ-15).
export interface ServiceResult {
  message: string;
  active_state?: string;
}
