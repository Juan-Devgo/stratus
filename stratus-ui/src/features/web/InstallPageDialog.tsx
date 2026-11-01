import { TriangleAlertIcon } from "lucide-react";
import { useState } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";

import { installPage } from "./api";
import { validatePageFile } from "./validation";

// vm es el servidor web cuyo diálogo está abierto, o null si está cerrado.
type Props = { vm: string | null; onClose(): void; onDone(): void };

// "Instalar página" (REQ-9, REQ-10) en dos pasos: 1) el archivo .tar.gz, validado antes de enviar
// nada; 2) la advertencia de que se reemplaza todo /var/www/html. Solo se sube al confirmar. El
// resultado se muestra en el mismo diálogo, que se cierra con "Cerrar".
export function InstallPageDialog({ vm, onClose, onDone }: Props) {
  return (
    <Dialog open={vm !== null} onOpenChange={(open) => !open && onClose()} disablePointerDismissal>
      {/* La clave reinicia el archivo y el resultado con cada servidor. */}
      {vm !== null && <InstallContent key={vm} vm={vm} onClose={onClose} onDone={onDone} />}
    </Dialog>
  );
}

function InstallContent({ vm, onClose, onDone }: Props & { vm: string }) {
  const [archive, setArchive] = useState<File | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const { pending, result, run } = useOperation();
  const disabled = pending || result !== null;

  function next() {
    const found = validatePageFile(archive);
    setError(found);
    if (!found) {
      setConfirming(true);
    }
  }

  async function confirm() {
    if (!archive) {
      return;
    }
    await run(() => installPage(vm, archive));
    onDone();
  }

  return (
    <DialogContent className="sm:max-w-lg" showCloseButton={false}>
      <DialogHeader>
        <DialogTitle>Instalar página en {vm}</DialogTitle>
        <DialogDescription>
          Copia un sitio estático en /var/www/html. El archivo .tar.gz debe tener index.html en la raíz.
        </DialogDescription>
      </DialogHeader>

      {confirming ? (
        <div className="flex flex-col gap-3">
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertDescription>
              Se reemplazará todo /var/www/html de {vm} con el contenido del archivo. Si algo falla, se restaura el
              contenido anterior.
            </AlertDescription>
          </Alert>
          <p className="text-sm text-muted-foreground">Archivo: {archive?.name}.</p>
          <OperationResult result={result} />
        </div>
      ) : (
        <Field data-invalid={error ? true : undefined}>
          <FieldLabel htmlFor="install-page-archive">Archivo de la página (.tar.gz)</FieldLabel>
          <Input
            id="install-page-archive"
            type="file"
            accept=".gz,.tgz"
            onChange={(event) => setArchive(event.target.files?.[0] ?? null)}
            aria-invalid={error ? true : undefined}
          />
          {error && <FieldError>{error}</FieldError>}
          <FieldDescription>Hasta 200 MB.{archive ? ` Elegido: ${archive.name}.` : ""}</FieldDescription>
        </Field>
      )}

      <DialogFooter>
        {result !== null ? (
          <Button onClick={onClose}>Cerrar</Button>
        ) : confirming ? (
          <>
            <Button variant="outline" disabled={disabled} onClick={() => setConfirming(false)}>
              Atrás
            </Button>
            <Button variant="destructive" disabled={disabled} onClick={() => void confirm()}>
              {pending && <Spinner data-icon="inline-start" aria-label="Cargando" />}
              Confirmar
            </Button>
          </>
        ) : (
          <>
            <Button variant="outline" onClick={onClose}>
              Cancelar
            </Button>
            <Button onClick={next}>Continuar</Button>
          </>
        )}
      </DialogFooter>
    </DialogContent>
  );
}
