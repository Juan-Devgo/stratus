import { TriangleAlertIcon } from "lucide-react";
import { useState } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useOperation } from "@/features/prepare/useOperation";

import { redeployService } from "./api";
import { StateWarning } from "./StateWarning";
import type { ServiceRow } from "./types";
import { redeployErrors, type ServiceFormErrors } from "./validation";

type Props = { vm: string; service: ServiceRow | null; onClose(): void; onDone(): void };

// Redespliegue (REQ-24) en dos pasos: 1) el archivo .tar.gz nuevo y el ejecutable, con el actual ya
// escrito; 2) una advertencia de que se reemplaza todo /opt/<name>, también los archivos de datos.
// El script solo se invoca al confirmar. El resultado se muestra en el mismo diálogo, que se cierra
// con "Cerrar".
export function RedeployServiceDialog({ vm, service, onClose, onDone }: Props) {
  return (
    <Dialog open={service !== null} onOpenChange={(open) => !open && onClose()} disablePointerDismissal>
      {/* La clave reinicia el formulario y el resultado con cada servicio. */}
      {service !== null && <RedeployContent key={service.name} vm={vm} service={service} onClose={onClose} onDone={onDone} />}
    </Dialog>
  );
}

function RedeployContent({ vm, service, onClose, onDone }: Props & { service: ServiceRow }) {
  const [executable, setExecutable] = useState(service.executable);
  const [archive, setArchive] = useState<File | null>(null);
  const [errors, setErrors] = useState<ServiceFormErrors>({});
  const [confirming, setConfirming] = useState(false);
  const [activeState, setActiveState] = useState<string | undefined>();
  const { pending, result, run } = useOperation();
  // Con un resultado a la vista el formulario queda solo de lectura.
  const disabled = pending || result !== null;

  function next() {
    const found = redeployErrors({ executable, archive });
    setErrors(found);
    if (Object.keys(found).length === 0) {
      setConfirming(true);
    }
  }

  async function confirm() {
    if (!archive) {
      return;
    }
    await run(async () => {
      const done = await redeployService(vm, service.name, executable, archive);
      setActiveState(done.active_state);
      return done.message;
    });
    onDone();
  }

  return (
    <DialogContent className="sm:max-w-lg" showCloseButton={false}>
      <DialogHeader>
        <DialogTitle>Redesplegar el servicio {service.name}</DialogTitle>
        <DialogDescription>
          Reemplaza la aplicación del servicio en la VM {vm} con un archivo .tar.gz nuevo. Si el servicio está en
          ejecución, se detiene y se vuelve a iniciar.
        </DialogDescription>
      </DialogHeader>

      {confirming ? (
        <div className="flex flex-col gap-3">
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertDescription>
              Se reemplaza todo el contenido de /opt/{service.name}, incluidos los archivos de datos. Si algo falla, se
              restaura el contenido anterior.
            </AlertDescription>
          </Alert>
          <p className="text-sm text-muted-foreground">
            Archivo: {archive?.name}. Ejecutable: {executable}.
          </p>
          <OperationResult result={result} />
          {result?.ok && <StateWarning name={service.name} state={activeState} />}
        </div>
      ) : (
        <FieldGroup>
          <Field data-invalid={errors.archive ? true : undefined}>
            <FieldLabel htmlFor="redeploy-service-archive">Archivo de la aplicación (.tar.gz)</FieldLabel>
            <Input
              id="redeploy-service-archive"
              type="file"
              accept=".gz,.tgz"
              onChange={(event) => setArchive(event.target.files?.[0] ?? null)}
              aria-invalid={errors.archive ? true : undefined}
            />
            {errors.archive && <FieldError>{errors.archive}</FieldError>}
            <FieldDescription>
              Hasta 200 MB.{archive ? ` Elegido: ${archive.name}.` : ""}
            </FieldDescription>
          </Field>
          <Field data-invalid={errors.executable ? true : undefined}>
            <FieldLabel htmlFor="redeploy-service-executable">Ejecutable</FieldLabel>
            <Input
              id="redeploy-service-executable"
              autoComplete="off"
              value={executable}
              onChange={(event) => setExecutable(event.target.value)}
              aria-invalid={errors.executable ? true : undefined}
            />
            {errors.executable && <FieldError>{errors.executable}</FieldError>}
            <FieldDescription>Ruta relativa a /opt/{service.name}; debe estar en el archivo nuevo.</FieldDescription>
          </Field>
        </FieldGroup>
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
