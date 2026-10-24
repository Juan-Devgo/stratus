// Listas permitidas del módulo prepare (P3). Son las mismas reglas de
// stratus-srv/internal/service/prepare_validation.go y de cada script; cada función
// devuelve el mensaje de error en español o null si el valor es válido.

import type { IssueField } from "./types";

const VM_NAME_PATTERN = /^[a-z0-9][a-z0-9-]{0,31}$/;
const PACKAGE_PATTERN = /^[a-z0-9][a-z0-9+.-]{1,62}$/;
// Una sola etiqueta DNS de 1 a 63 caracteres, sin guion al inicio ni al final (REQ-12).
const HOSTNAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
// Mensaje de /etc/issue: letras, marcas, números, signos, símbolos, el espacio y saltos de línea.
// Es el mismo conjunto que unicode.IsPrint de Go más "\n"; la barra invertida se rechaza aparte.
const ISSUE_MESSAGE_PATTERN = /^[\p{L}\p{M}\p{N}\p{P}\p{S} \n]*$/u;
const MAX_ISSUE_MESSAGE = 500;

export interface CreateVmInput {
  name: string;
  memory_mb: number;
  cpus: number;
}

export type CreateVmErrors = Partial<Record<keyof CreateVmInput, string>>;

export function validateVmName(name: string): string | null {
  if (!VM_NAME_PATTERN.test(name)) {
    return "El nombre de la VM debe tener de 1 a 32 caracteres: minúsculas, números y guiones, sin empezar con guion";
  }
  return null;
}

// Mensaje si value no es un entero entre min y max.
function rangeError(label: string, unit: string, value: number, min: number, max: number): string | null {
  if (Number.isInteger(value) && value >= min && value <= max) {
    return null;
  }
  return `${label} debe estar entre ${min} y ${max}${unit ? ` ${unit}` : ""}`;
}

// Errores por campo, para mostrar cada uno bajo su control en el formulario.
export function createVmErrors(input: CreateVmInput): CreateVmErrors {
  const errors: CreateVmErrors = {};
  const checks: [keyof CreateVmInput, string | null][] = [
    ["name", validateVmName(input.name)],
    ["memory_mb", rangeError("La memoria", "MB", input.memory_mb, 512, 8192)],
    ["cpus", rangeError("El número de CPUs", "", input.cpus, 1, 4)],
  ];
  for (const [field, error] of checks) {
    if (error) {
      errors[field] = error;
    }
  }
  return errors;
}

// Primer error de la creación de VM, o null si todo es válido.
export function validateCreateVm(input: CreateVmInput): string | null {
  return Object.values(createVmErrors(input))[0] ?? null;
}

// Nombre de paquete Debian (REQ-4): rechaza vacío, espacios y metacaracteres como ";".
export function validatePackage(pkg: string): string | null {
  if (!PACKAGE_PATTERN.test(pkg)) {
    return 'El nombre del paquete debe tener de 2 a 63 caracteres: minúsculas, números, "+", "." y "-", empezando con letra o número';
  }
  return null;
}

// Hostname de la VM (REQ-12): rechaza mayúsculas, "_", puntos y guion al inicio o al final.
export function validateHostname(hostname: string): string | null {
  if (!HOSTNAME_PATTERN.test(hostname)) {
    return "El hostname debe tener de 1 a 63 caracteres: minúsculas, números y guiones, sin empezar ni terminar con guion";
  }
  return null;
}

export interface IssueInput {
  message: string;
  fields: IssueField[];
}

// Contenido de /etc/issue (REQ-10): al menos un campo o un mensaje; el mensaje tiene hasta 500
// caracteres imprimibles o saltos de línea y no lleva "\", que agetty tomaría como escape.
export function validateIssue({ message, fields }: IssueInput): string | null {
  if (message === "" && fields.length === 0) {
    return "Seleccione al menos un campo o escriba un mensaje";
  }
  // Se cuentan caracteres Unicode, no unidades UTF-16, igual que el backend.
  if ([...message].length > MAX_ISSUE_MESSAGE) {
    return `El mensaje debe tener como máximo ${MAX_ISSUE_MESSAGE} caracteres`;
  }
  if (message.includes("\\") || !ISSUE_MESSAGE_PATTERN.test(message)) {
    return "El mensaje solo admite caracteres imprimibles y saltos de línea, sin barra invertida (\\)";
  }
  return null;
}
