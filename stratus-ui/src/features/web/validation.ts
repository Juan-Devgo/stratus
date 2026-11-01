// Listas permitidas del módulo de servidores web (P3). Son las mismas reglas de
// stratus-srv/internal/service/web_validation.go y de scripts/web/lib/web-common.sh; los campos de
// la VM reusan las de 001 y el archivo de la página, las de 003.
import { createVmErrors } from "@/features/prepare/validation";
import { validateArchive } from "@/features/services/validation";

import type { CreateWebInput } from "./types";

export type CreateWebErrors = Partial<Record<keyof CreateWebInput, string>>;

// Errores por campo de "Crear servidor web": los de 001 más el tipo obligatorio (REQ-1).
export function createWebErrors(input: CreateWebInput): CreateWebErrors {
  const errors: CreateWebErrors = createVmErrors(input);
  if (input.type !== "apache" && input.type !== "nginx") {
    errors.type = "Seleccione el tipo de servidor web";
  }
  return errors;
}

// El archivo de la página: .tar.gz de hasta 200 MB, antes de enviar nada (REQ-9).
export function validatePageFile(file: File | null): string | null {
  if (!file) {
    return "Seleccione el archivo .tar.gz de la página";
  }
  return validateArchive(file);
}
