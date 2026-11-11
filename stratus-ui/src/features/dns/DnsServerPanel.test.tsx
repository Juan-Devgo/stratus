import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { DnsServerPanel } from "./DnsServerPanel";
import type { DnsServer } from "./types";

const SERVER: DnsServer = { name: "dns1", state: "running", ip: "192.168.1.53", zone: "stratus.lan" };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function actions() {
  return screen.getByRole("group", { name: "Acciones de dns1" });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("DnsServerPanel", () => {
  it("AC-17 shows the name, state, IPv4 and zone", () => {
    render(<DnsServerPanel server={SERVER} busy={false} onDone={vi.fn()} />);
    for (const text of ["dns1", "Encendida", "192.168.1.53", "stratus.lan"]) {
      expect(screen.getByText(text)).toBeInTheDocument();
    }
  });

  it("AC-39 Actualizar registros posts the refresh and shows the result", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "Registros sin cambios" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<DnsServerPanel server={SERVER} busy={false} onDone={onDone} />);

    await user.click(within(actions()).getByRole("button", { name: "Actualizar registros" }));

    expect(await screen.findByText("Registros sin cambios")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/dns/records/refresh");
    expect(init?.method).toBe("POST");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-58 refresh result names the removed VMs", async () => {
    const message = "Registros actualizados; registros quitados: web-a, vm3";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message }));
    const user = userEvent.setup();
    render(<DnsServerPanel server={SERVER} busy={false} onDone={vi.fn()} />);

    await user.click(within(actions()).getByRole("button", { name: "Actualizar registros" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
  });

  it("AC-29 delete names the server, waits for Confirmar and shows the rejection", async () => {
    const text = "Operación en curso en esta VM";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: text }));
    const user = userEvent.setup();
    render(<DnsServerPanel server={SERVER} busy={false} onDone={vi.fn()} />);

    await user.click(within(actions()).getByRole("button", { name: "Eliminar" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("¿Eliminar el servidor DNS dns1?")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(text)).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/dns/server");
    expect(init?.method).toBe("DELETE");
  });

  it("AC-19 Reiniciar calls the bind action", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "named reiniciado en dns1", active_state: "active" }));
    const user = userEvent.setup();
    render(<DnsServerPanel server={SERVER} busy={false} onDone={vi.fn()} />);

    await user.click(within(actions()).getByRole("button", { name: "Reiniciar" }));

    expect(await screen.findByText("named reiniciado en dns1")).toBeInTheDocument();
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/dns/server/restart");
  });
});
