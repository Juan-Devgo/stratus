// Listas permitidas del módulo HAProxy (P3). Son las mismas reglas de
// stratus-srv/internal/service/haproxy_validation.go y de scripts/haproxy/lib/haproxy-common.sh;
// cada función recibe el texto del campo y devuelve el mensaje de error en español o null.

const PORT_PATTERN = /^[1-9][0-9]{0,4}$/;
const SSH_PORT = 22;

// Puerto de un servidor: un entero de 1 a 65535 (REQ-8).
export function validatePort(text: string): string | null {
  if (!PORT_PATTERN.test(text) || Number(text) > 65535) {
    return "El puerto debe ser un número entre 1 y 65535";
  }
  return null;
}

// Puerto del frontend: como validatePort y distinto de 22, que es el de SSH (REQ-7).
export function validateFrontendPort(text: string): string | null {
  const error = validatePort(text);
  if (error) {
    return error;
  }
  return Number(text) === SSH_PORT ? "El puerto 22 está reservado para SSH" : null;
}
