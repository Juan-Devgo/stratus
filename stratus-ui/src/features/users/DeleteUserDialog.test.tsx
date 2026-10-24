import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DeleteUserDialog } from "./DeleteUserDialog";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("DeleteUserDialog", () => {
  it("AC-16 delete asks for confirmation naming the user", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Usuario luis eliminado de vm1" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<DeleteUserDialog vm="vm1" username="luis" onClose={vi.fn()} onDone={onDone} />);

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("luis");
    expect(screen.getByRole("checkbox", { name: "Eliminar todos sus archivos" })).not.toBeChecked();
    // Nada se envía hasta confirmar.
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Usuario luis eliminado de vm1")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/users/vms/vm1/users/luis?remove_files=false");
    expect(init?.method).toBe("DELETE");
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("AC-16 the checkbox sends remove_files=true", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Usuario luis eliminado de vm1 junto con todos sus archivos" }));
    const user = userEvent.setup();
    render(<DeleteUserDialog vm="vm1" username="luis" onClose={vi.fn()} onDone={vi.fn()} />);

    await user.click(await screen.findByRole("checkbox", { name: "Eliminar todos sus archivos" }));
    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(/junto con todos sus archivos/)).toBeInTheDocument();
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/users/vms/vm1/users/luis?remove_files=true");
  });

  it("AC-16 cancel closes without calling the API", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<DeleteUserDialog vm="vm1" username="luis" onClose={onClose} onDone={vi.fn()} />);

    await user.click(await screen.findByRole("button", { name: "Cancelar" }));

    expect(onClose).toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-17 shows the failure message from the backend", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(409, { error: "Solo se pueden eliminar usuarios regulares existentes" }),
    );
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<DeleteUserDialog vm="vm1" username="luis" onClose={vi.fn()} onDone={onDone} />);

    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Solo se pueden eliminar usuarios regulares existentes")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("AC-16 renders nothing while no user is selected", () => {
    render(<DeleteUserDialog vm="vm1" username={null} onClose={vi.fn()} onDone={vi.fn()} />);

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });
});
