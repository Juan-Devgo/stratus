import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";

import { CreateVmForm } from "./CreateVmForm";
import type { OperationOutcome } from "./useOperation";
import type { CreateVmInput } from "./validation";

type Props = {
  open: boolean;
  onOpenChange(open: boolean): void;
  pending: boolean;
  result: OperationOutcome | null;
  onSubmit(input: CreateVmInput): void;
};

// Diálogo con la carta del formulario "Crear VM". Cerrarlo no cancela la creación: el estado
// de la operación lo guarda la pantalla y CreateVmStatus lo sigue mostrando.
export function CreateVmDialog({ open, onOpenChange, pending, result, onSubmit }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 p-0 sm:max-w-lg" showCloseButton={false}>
        <Card className="ring-0">
          <CardHeader>
            <CardTitle>
              <DialogTitle>Crear VM</DialogTitle>
            </CardTitle>
            <CardDescription>
              <DialogDescription>
                Crea un clon de Debian 13 sobre el disco base compartido (disco diferencial, red en puente, IP por DHCP)
                y lo prepara para el acceso por SSH. Puede tardar varios minutos.
              </DialogDescription>
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <CreateVmForm pending={pending} result={result} onSubmit={onSubmit} />
            <div className="flex justify-end">
              <DialogClose render={<Button variant="outline" />}>Cerrar</DialogClose>
            </div>
          </CardContent>
        </Card>
      </DialogContent>
    </Dialog>
  );
}
