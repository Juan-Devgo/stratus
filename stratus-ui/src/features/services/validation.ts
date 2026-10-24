// Listas permitidas del módulo de servicios (P3). Son las mismas reglas de
// stratus-srv/internal/service/services_validation.go y de scripts/services/lib/service-common.sh;
// cada función devuelve el mensaje de error en español o null si el valor es válido. Los mensajes
// de las variables de entorno nunca repiten el valor (REQ-13).
import type { EnvVar } from "./types";

const SERVICE_NAME_PATTERN = /^[a-z][a-z0-9-]{0,31}$/;
const EXECUTABLE_PATTERN = /^[A-Za-z0-9._-]+(\/[A-Za-z0-9._-]+)*$/;
const ARG_TOKEN_PATTERN = /^[A-Za-z0-9._/:=,@+-]{1,128}$/;
const ENV_KEY_PATTERN = /^[A-Z_][A-Z0-9_]{0,63}$/;
// Valores de variables: ASCII imprimible (espacio a ~), sin saltos de línea.
const ENV_VALUE_PATTERN = /^[\x20-\x7E]*$/;
// Descripción: sin caracteres de control, de formato ni sin asignar, y sin % \ ".
const DESCRIPTION_FORBIDDEN = /[\p{C}%\\"]/u;
const RESTART_SEC_PATTERN = /^(0|[1-9][0-9]{0,3})$/;

export const MAX_ARCHIVE_BYTES = 200 * 1024 * 1024;
const MAX_ENV_ROWS = 50;
const MAX_ENV_VALUE = 1024;

export function validateServiceName(name: string): string | null {
  if (!SERVICE_NAME_PATTERN.test(name)) {
    return 'El nombre del servicio debe tener de 1 a 32 caracteres: minúsculas, números y "-", empezando con una letra';
  }
  return null;
}

export function validateDescription(description: string): string | null {
  const length = [...description].length; // en caracteres, no en unidades UTF-16
  if (length < 1 || length > 100 || DESCRIPTION_FORBIDDEN.test(description)) {
    return 'La descripción debe tener de 1 a 100 caracteres imprimibles, sin %, \\, " ni saltos de línea';
  }
  return null;
}

export function validateExecutable(executable: string): string | null {
  const message =
    'El ejecutable debe ser una ruta relativa de hasta 255 caracteres con letras, números, ".", "_", "-" y "/", sin segmentos . ni ..';
  if (executable.length > 255 || !EXECUTABLE_PATTERN.test(executable)) {
    return message;
  }
  if (executable.split("/").some((segment) => segment === "." || segment === "..")) {
    return message;
  }
  return null;
}

export function validateArgs(args: string): string | null {
  if (args === "") {
    return null;
  }
  const tokens = args.split(" ");
  if (tokens.length > 16 || tokens.some((token) => !ARG_TOKEN_PATTERN.test(token))) {
    return "Los argumentos deben ser hasta 16 tokens de 1 a 128 caracteres (letras, números y . _ / : = , @ + -) separados por un espacio";
  }
  return null;
}

export function validateRestart(restart: string): string | null {
  return ["no", "always", "on-failure"].includes(restart) ? null : "Restart debe ser no, always u on-failure";
}

// Recibe el texto del campo: un entero de 0 a 3600 sin ceros a la izquierda.
export function validateRestartSec(text: string): string | null {
  if (!RESTART_SEC_PATTERN.test(text) || Number(text) > 3600) {
    return "RestartSec debe ser un entero de 0 a 3600";
  }
  return null;
}

// El archivo del navegador: extensión .tar.gz y hasta 200 MB, antes de enviar nada (REQ-4).
export function validateArchive(file: File | null): string | null {
  if (!file) {
    return "Seleccione el archivo .tar.gz de la aplicación";
  }
  if (!file.name.endsWith(".tar.gz")) {
    return "El archivo debe ser .tar.gz";
  }
  if (file.size > MAX_ARCHIVE_BYTES) {
    return "El archivo supera 200 MB";
  }
  return null;
}

export interface EnvRowsErrors {
  rows: (string | null)[]; // un error por fila, en el mismo orden
  list: string | null; // error del conjunto (más de 50 filas)
}

// Valida cada fila (clave, unicidad y valor) y el número de filas (REQ-9).
export function validateEnvRows(rows: EnvVar[]): EnvRowsErrors {
  const seen = new Set<string>();
  const errors = rows.map((row, index) => {
    const label = `La variable ${index + 1}`;
    let error: string | null = null;
    if (!ENV_KEY_PATTERN.test(row.key)) {
      error = `${label}: la clave debe cumplir ^[A-Z_][A-Z0-9_]{0,63}$`;
    } else if (seen.has(row.key)) {
      error = `${label}: la clave ${row.key} está repetida`;
    } else if (row.value.length > MAX_ENV_VALUE || !ENV_VALUE_PATTERN.test(row.value)) {
      error = `${label}: el valor admite hasta 1024 caracteres ASCII imprimibles`;
    }
    seen.add(row.key);
    return error;
  });
  return { rows: errors, list: rows.length > MAX_ENV_ROWS ? "Se permiten hasta 50 variables de entorno" : null };
}

// Campos de las unidades que comparten crear y editar; restart_sec es el texto del campo.
interface UnitFormFields {
  description: string;
  executable: string;
  args: string;
  restart: string;
  restart_sec: string;
}

export interface CreateServiceFormValues extends UnitFormFields {
  name: string;
  archive: File | null;
  env: EnvVar[];
}

export interface EditServiceFormValues extends UnitFormFields {
  replace_env: boolean;
  env: EnvVar[];
}

export interface RedeployFormValues {
  executable: string;
  archive: File | null;
}

// Errores por campo, para mostrar cada uno bajo su control. Las claves son el nombre del campo;
// el error de la fila i de variables es env_<i> y el del conjunto es env.
export type ServiceFormErrors = Record<string, string>;

function setError(errors: ServiceFormErrors, key: string, message: string | null) {
  if (message) {
    errors[key] = message;
  }
}

function unitErrors(form: UnitFormFields, errors: ServiceFormErrors) {
  setError(errors, "description", validateDescription(form.description));
  setError(errors, "executable", validateExecutable(form.executable));
  setError(errors, "args", validateArgs(form.args));
  setError(errors, "restart", validateRestart(form.restart));
  setError(errors, "restart_sec", validateRestartSec(form.restart_sec));
}

function envErrors(rows: EnvVar[], errors: ServiceFormErrors) {
  const result = validateEnvRows(rows);
  result.rows.forEach((message, index) => setError(errors, `env_${index}`, message));
  setError(errors, "env", result.list);
}

export function createServiceErrors(form: CreateServiceFormValues): ServiceFormErrors {
  const errors: ServiceFormErrors = {};
  setError(errors, "name", validateServiceName(form.name));
  unitErrors(form, errors);
  setError(errors, "archive", validateArchive(form.archive));
  envErrors(form.env, errors);
  return errors;
}

// Sin "Reemplazar variables de entorno" las filas son solo las claves actuales: no se validan.
export function editServiceErrors(form: EditServiceFormValues): ServiceFormErrors {
  const errors: ServiceFormErrors = {};
  unitErrors(form, errors);
  if (form.replace_env) {
    envErrors(form.env, errors);
  }
  return errors;
}

export function redeployErrors(form: RedeployFormValues): ServiceFormErrors {
  const errors: ServiceFormErrors = {};
  setError(errors, "executable", validateExecutable(form.executable));
  setError(errors, "archive", validateArchive(form.archive));
  return errors;
}
