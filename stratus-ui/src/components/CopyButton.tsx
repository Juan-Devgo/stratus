import { CheckIcon, CopyIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";

// Tiempo que se muestra el check después de copiar (REQ-44).
const COPIED_MS = 2000;

// Copia el texto al portapapeles. navigator.clipboard solo existe en un contexto seguro (HTTPS o
// localhost); si la UI se abre por HTTP desde una IP de la LAN, se usa un textarea oculto con
// execCommand("copy"). Lanza un error si el navegador no pudo copiar.
async function copyText(value: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const area = document.createElement("textarea");
  area.value = value;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.focus();
  area.select();
  try {
    if (!document.execCommand("copy")) {
      throw new Error("execCommand devolvió false");
    }
  } finally {
    area.remove();
  }
}

// Botón de ícono que copia un valor (una IP, REQ-43) y muestra un check unos 2 segundos; si no se
// pudo copiar, avisa con un toast (REQ-44).
export function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);

  // Vuelve al ícono de copiar después de COPIED_MS; se cancela si el botón se desmonta antes.
  useEffect(() => {
    if (!copied) {
      return;
    }
    const timer = setTimeout(() => setCopied(false), COPIED_MS);
    return () => clearTimeout(timer);
  }, [copied]);

  async function copy() {
    try {
      await copyText(value);
      setCopied(true);
    } catch {
      toast.error(`No se pudo copiar ${value}`);
    }
  }

  return (
    <Button
      variant="ghost"
      size="icon-xs"
      aria-label={`Copiar ${value}`}
      title={`Copiar ${value}`}
      data-copied={copied}
      onClick={() => void copy()}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}
