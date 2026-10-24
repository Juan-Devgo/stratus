import type { IssueField } from "./types";

// Campos dinámicos de /etc/issue en el orden fijo de REQ-26. "escape" es la secuencia que
// agetty reemplaza al mostrar la pantalla de inicio de sesión (por ejemplo \n por el hostname).
// set-issue.sh escribe las mismas líneas "<label>: <escape>".
export const ISSUE_FIELDS: readonly { key: IssueField; label: string; escape: string }[] = [
  { key: "hostname", label: "Host", escape: "\\n" },
  { key: "date", label: "Fecha", escape: "\\d" },
  { key: "time", label: "Hora", escape: "\\t" },
  { key: "ipv4", label: "IPv4", escape: "\\4" },
  { key: "os", label: "Sistema", escape: "\\s" },
  { key: "kernel", label: "Kernel", escape: "\\r" },
  { key: "arch", label: "Arquitectura", escape: "\\m" },
  { key: "tty", label: "Terminal", escape: "\\l" },
  { key: "users", label: "Usuarios", escape: "\\U" },
];

// Contenido de /etc/issue: primero el mensaje (si hay) y luego una línea por campo marcado,
// siempre en el orden de ISSUE_FIELDS sin importar el orden en que se marcaron.
export function composeIssue(message: string, fields: IssueField[]): string {
  const lines = message === "" ? [] : [message];
  for (const field of ISSUE_FIELDS) {
    if (fields.includes(field.key)) {
      lines.push(`${field.label}: ${field.escape}`);
    }
  }
  return lines.join("\n");
}
