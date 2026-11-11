import { CopyButton } from "./CopyButton";

// Contenido de una celda de IP en cualquier tabla (REQ-43): "-" sin botón si no hay IP; si la hay,
// la IP con su botón de copiar.
export function IpCell({ ip }: { ip: string }) {
  if (!ip) {
    return <>-</>;
  }
  return (
    <span className="inline-flex items-center gap-1">
      <span>{ip}</span>
      <CopyButton value={ip} />
    </span>
  );
}
