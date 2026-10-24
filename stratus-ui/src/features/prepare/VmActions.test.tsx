import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "./types";
import { VmActions } from "./VmActions";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("VmActions", () => {
  it("AC-4 delete asks for confirmation first", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "VM vm1 eliminada" }));
    const user = userEvent.setup();
    render(<VmActions vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Eliminar" }));

    expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1");
    expect(init?.method).toBe("DELETE");
  });

  it("AC-47 start shows result", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "VM vm1 encendida" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<VmActions vm={{ ...vm1, state: "poweroff", ip: "" }} onDone={onDone} />);

    await user.click(screen.getByRole("button", { name: "Iniciar" }));

    expect(await screen.findByText("VM vm1 encendida")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1/start");
    expect(init?.method).toBe("POST");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-41 start shows the success result and the warning when the refresh failed", async () => {
    const warning = "VM iniciada; no se pudieron actualizar las IPs del balanceador lb1, use Actualizar IPs";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "VM web-a encendida", warning }));
    const user = userEvent.setup();
    render(<VmActions vm={{ ...vm1, name: "web-a", role: "server", lb: "lb1" }} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Iniciar" }));

    expect(await screen.findByText("VM web-a encendida")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    expect(screen.getByText(warning)).toBeInTheDocument();
    expect(screen.getByText("Advertencia")).toBeInTheDocument();
  });

  it("AC-41 start without warning shows no warning, and a later operation clears an old one", async () => {
    const warning = "VM iniciada; no se pudieron actualizar las IPs del balanceador lb1, use Actualizar IPs";
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse(200, { message: "VM vm1 encendida", warning }))
      .mockResolvedValueOnce(jsonResponse(200, { message: "VM vm1 apagada" }));
    const user = userEvent.setup();
    render(<VmActions vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Iniciar" }));
    expect(await screen.findByText(warning)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Detener" }));
    expect(await screen.findByText("VM vm1 apagada")).toBeInTheDocument();
    expect(screen.queryByText(warning)).not.toBeInTheDocument();
    expect(screen.queryByText("Advertencia")).not.toBeInTheDocument();
  });

  it("AC-47 start of a plain VM shows no warning", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "VM vm1 encendida" }));
    const user = userEvent.setup();
    render(<VmActions vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Iniciar" }));

    expect(await screen.findByText("VM vm1 encendida")).toBeInTheDocument();
    expect(screen.queryByText("Advertencia")).not.toBeInTheDocument();
  });

  it("AC-5 disables actions without selected VM", () => {
    render(<VmActions vm={null} onDone={vi.fn()} />);

    for (const name of ["Iniciar", "Detener", "Eliminar"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-30 stop shows result", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "VM vm1 apagada" }));
    const user = userEvent.setup();
    render(<VmActions vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Detener" }));

    expect(await screen.findByText("VM vm1 apagada")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1/stop");
    expect(init?.method).toBe("POST");
  });

  it("AC-31 confirmed delete shows result", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "VM vm1 eliminada junto con su disco" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<VmActions vm={vm1} onDone={onDone} />);

    await user.click(screen.getByRole("button", { name: "Eliminar" }));
    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("VM vm1 eliminada junto con su disco")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-38 delete dialog states the base disk is untouched", async () => {
    const user = userEvent.setup();
    render(<VmActions vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Eliminar" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("disco diferencial");
    expect(dialog).toHaveTextContent("El disco base compartido no se modifica");
    expect(dialog).toHaveTextContent("no se puede deshacer");
  });
});
