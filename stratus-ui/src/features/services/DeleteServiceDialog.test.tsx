import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DeleteServiceDialog } from "./DeleteServiceDialog";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("DeleteServiceDialog", () => {
  it("AC-28 asks for confirmation naming the service and sends nothing until Confirmar", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Servicio api eliminado de vm1" }));
    const onDone = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(<DeleteServiceDialog vm="vm1" name="api" onClose={vi.fn()} onDone={onDone} />);

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("api");
    expect(dialog).toHaveTextContent("/opt/api");
    expect(dialog).toHaveTextContent("/etc/api.env");
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Servicio api eliminado de vm1")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/services/vms/vm1/services/api");
    expect(init?.method).toBe("DELETE");
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Cerrar" })).toBeInTheDocument();
  });

  it("AC-41 a failed delete shows the message naming the service", async () => {
    const error = "La operación delete_service falló (código 5): No se pudo eliminar el servicio api: falló el paso 5";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(500, { error }));
    const onDone = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(<DeleteServiceDialog vm="vm1" name="api" onClose={vi.fn()} onDone={onDone} />);

    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(error)).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    // El servicio sigue instalado: la lista se vuelve a leer y el borrado se puede repetir.
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("Cancelar closes without sending anything", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const onClose = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(<DeleteServiceDialog vm="vm1" name="api" onClose={onClose} onDone={vi.fn()} />);

    await user.click(await screen.findByRole("button", { name: "Cancelar" }));

    expect(onClose).toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-37 shows loading and disables the buttons while deleting", async () => {
    let resolve: (response: Response) => void = () => undefined;
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise((r) => (resolve = r)));
    const user = userEvent.setup({ delay: null });
    render(<DeleteServiceDialog vm="vm1" name="api" onClose={vi.fn()} onDone={vi.fn()} />);

    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByLabelText("Cargando")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Confirmar/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancelar" })).toBeDisabled();

    resolve(jsonResponse(200, { message: "Servicio api eliminado de vm1" }));
    expect(await screen.findByRole("button", { name: "Cerrar" })).toBeEnabled();
  });

  it("renders nothing without a service", () => {
    render(<DeleteServiceDialog vm="vm1" name={null} onClose={vi.fn()} onDone={vi.fn()} />);

    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
});
