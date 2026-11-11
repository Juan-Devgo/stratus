import { ActivityIcon, PlayIcon, RotateCwIcon, SquareIcon } from "lucide-react";
import { useState, type ReactNode } from "react";

import { OperationResult } from "@/components/OperationResult";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { ApiError } from "@/features/prepare/api";
import { useOperation } from "@/features/prepare/useOperation";
import { ServiceOutputPanel } from "@/features/services/ServiceOutputPanel";
import { StateWarning } from "@/features/services/StateWarning";
import type { ServiceResult } from "@/features/services/types";

import type { UnitAction } from "./types";

type Props = {
  name: string; // VM cuyo servicio se controla
  unit: string; // apache2, nginx o named, para el aviso de estado
  busy: boolean;
  onAction(action: UnitAction): Promise<ServiceResult>;
  onStatus(): Promise<string>;
  onDone(): void;
  children?: ReactNode; // botones extra de la misma fila, por ejemplo "Instalar página"
};

const ACTIONS: { action: UnitAction; label: string; icon: ReactNode }[] = [
  { action: "start", label: "Iniciar", icon: <PlayIcon data-icon="inline-start" /> },
  { action: "stop", label: "Detener", icon: <SquareIcon data-icon="inline-start" /> },
  { action: "restart", label: "Reiniciar", icon: <RotateCwIcon data-icon="inline-start" /> },
];

// Botones de systemctl de un servidor web o del servidor DNS (REQ-12): Iniciar, Detener y Reiniciar
// muestran el resultado y, si el servicio no quedó "active", el aviso de revisar Estado (AC-21);
// Estado abre la salida de "systemctl status", que se muestra aunque diga failed (AC-20).
export function UnitButtons({ name, unit, busy, onAction, onStatus, onDone, children }: Props) {
  const { pending, result, run } = useOperation();
  const [activeState, setActiveState] = useState<string | undefined>();
  const [running, setRunning] = useState<UnitAction | "status" | null>(null);
  const [output, setOutput] = useState<string | null>(null);
  const [statusError, setStatusError] = useState<string | null>(null);
  const disabled = busy || running !== null;

  async function act(action: UnitAction) {
    setRunning(action);
    setActiveState(undefined);
    setOutput(null);
    setStatusError(null);
    await run(async () => {
      const res = await onAction(action);
      setActiveState(res.active_state);
      return res.message;
    });
    setRunning(null);
    onDone();
  }

  async function status() {
    setRunning("status");
    setStatusError(null);
    try {
      setOutput(await onStatus());
    } catch (error) {
      setOutput(null);
      setStatusError(error instanceof ApiError ? error.message : "Ocurrió un error inesperado en la consola.");
    } finally {
      setRunning(null);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div role="group" aria-label={`Acciones de ${name}`} className="flex flex-wrap gap-2">
        {ACTIONS.map(({ action, label, icon }) => (
          <Button key={action} variant="outline" size="sm" disabled={disabled} onClick={() => void act(action)}>
            {running === action ? <Spinner data-icon="inline-start" aria-label="Cargando" /> : icon}
            {label}
          </Button>
        ))}
        <Button variant="outline" size="sm" disabled={disabled} onClick={() => void status()}>
          {running === "status" ? (
            <Spinner data-icon="inline-start" aria-label="Cargando" />
          ) : (
            <ActivityIcon data-icon="inline-start" />
          )}
          Estado
        </Button>
        {children}
      </div>
      {!pending && <OperationResult result={statusError ? { ok: false, text: statusError } : result} />}
      {result?.ok && !statusError && <StateWarning name={unit} state={activeState} hint="revise Estado" />}
      {output !== null && <ServiceOutputPanel title={`Estado de ${name}`} output={output} />}
    </div>
  );
}
