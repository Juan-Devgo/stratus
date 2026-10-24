import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { LbActions } from "./LbActions";
import type { Lb } from "./types";

const lb1: Lb = { name: "lb1", state: "running", ip: "192.168.1.60", subnet: "10.10.1.0/24", frontend_port: 80, server_count: 0 };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("LbActions", () => {
  it("AC-10 delete names the load balancer and its internal network and sends nothing until Confirmar", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Balanceador lb1 eliminado junto con su red interna" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<LbActions lb={lb1} onDone={onDone} />);

    await user.click(screen.getByRole("button", { name: "Eliminar" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("lb1");
    expect(dialog).toHaveTextContent("stratus-lb1");
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Balanceador lb1 eliminado junto con su red interna")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/haproxy/lbs/lb1");
    expect(init?.method).toBe("DELETE");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-10 shows the 409 text when the load balancer still has servers", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(409, { error: "Quite primero los servidores del balanceador" }),
    );
    const user = userEvent.setup();
    render(<LbActions lb={{ ...lb1, server_count: 1 }} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Eliminar" }));
    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Quite primero los servidores del balanceador")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("AC-10 Cancelar closes the dialog without calling the API", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<LbActions lb={lb1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Eliminar" }));
    await user.click(await screen.findByRole("button", { name: "Cancelar" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-22 Actualizar IPs posts to the refresh route and shows the result", async () => {
    const message = "IPs de lb1 actualizadas y HAProxy recargado";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<LbActions lb={lb1} onDone={onDone} />);

    await user.click(screen.getByRole("button", { name: "Actualizar IPs" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/haproxy/lbs/lb1/refresh");
    expect(init?.method).toBe("POST");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-22 shows the failure text of the refresh", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(409, { error: "El balanceador lb1 está apagado; enciéndalo para llegar al servidor" }),
    );
    const user = userEvent.setup();
    render(<LbActions lb={lb1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Actualizar IPs" }));

    expect(await screen.findByText(/está apagado/)).toBeInTheDocument();
  });

  it("disables both actions without a selected load balancer", () => {
    render(<LbActions lb={null} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Actualizar IPs" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Eliminar" })).toBeDisabled();
    expect(screen.getByText("Seleccione un balanceador para continuar")).toBeInTheDocument();
  });

  it("REQ-22 disables the buttons and shows a spinner while the refresh runs", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));
    const user = userEvent.setup();
    render(<LbActions lb={lb1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Actualizar IPs" }));

    expect(await screen.findByRole("status", { name: "Cargando" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Actualizar IPs/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Eliminar" })).toBeDisabled();
  });
});
