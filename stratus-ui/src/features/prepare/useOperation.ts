import { useCallback, useState } from "react";

import { ApiError } from "./api";

export interface OperationOutcome {
  ok: boolean;
  text: string;
}

// Estado de una operación de formulario: pending mientras espera al backend (hasta 15 min)
// y result con el mensaje de éxito o de fallo para mostrarlo en OperationResult.
export function useOperation() {
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<OperationOutcome | null>(null);

  const run = useCallback(async (fn: () => Promise<string>) => {
    setPending(true);
    setResult(null);
    try {
      setResult({ ok: true, text: await fn() });
    } catch (error) {
      const text = error instanceof ApiError ? error.message : "Ocurrió un error inesperado en la consola.";
      setResult({ ok: false, text });
    } finally {
      setPending(false);
    }
  }, []);

  return { pending, result, run };
}
