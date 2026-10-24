import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ServerTable } from "./ServerTable";
import type { LbServer } from "./types";

const servers: LbServer[] = [
  { name: "server1", vm: "web-a", ip: "10.10.1.11", port: 80, state: "UP" },
  { name: "server2", vm: "web-b", ip: "10.10.1.12", port: 8080, state: "DOWN" },
];

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ServerTable", () => {
  it("AC-25 shows name, VM, internal IP, port and state of each server", () => {
    render(<ServerTable servers={servers} lbOff={false} lb="lb1" onDone={vi.fn()} />);

    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent);
    expect(headers.slice(0, 5)).toEqual(["Servidor", "VM", "IP interna", "Puerto", "Estado"]);
    const [first, second] = screen.getAllByRole("row").slice(1);
    for (const text of ["server1", "web-a", "10.10.1.11", "80", "UP"]) {
      expect(within(first).getByText(text)).toBeInTheDocument();
    }
    for (const text of ["server2", "web-b", "10.10.1.12", "8080", "DOWN"]) {
      expect(within(second).getByText(text)).toBeInTheDocument();
    }
    expect(screen.queryByText(/El balanceador está apagado/)).not.toBeInTheDocument();
  });

  it("AC-46 shows - as the state and the off note when the load balancer is off", () => {
    const stored = servers.map((server) => ({ ...server, state: "-" }));
    render(<ServerTable servers={stored} lbOff={true} lb="lb1" onDone={vi.fn()} />);

    const [first] = screen.getAllByRole("row").slice(1);
    expect(within(first).getByText("-")).toBeInTheDocument();
    expect(screen.getByText("El balanceador está apagado; los datos son los últimos conocidos")).toBeInTheDocument();
  });

  it("shows an empty state without servers", () => {
    render(<ServerTable servers={[]} lbOff={false} lb="lb1" onDone={vi.fn()} />);

    expect(screen.getByText("Sin servidores")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("AC-18 Quitar asks for confirmation warning that the VM restarts, then calls DELETE", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Servidor web-a quitado de lb1" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<ServerTable servers={servers} lbOff={false} lb="lb1" onDone={onDone} />);

    await user.click(screen.getByRole("button", { name: "Quitar web-a" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("web-a");
    expect(dialog).toHaveTextContent(/se reinicia/i);
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Servidor web-a quitado de lb1")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/haproxy/lbs/lb1/servers/web-a");
    expect(init?.method).toBe("DELETE");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-40 shows the 409 text when the load balancer is off", async () => {
    const message = "El balanceador lb1 está apagado; enciéndalo para llegar al servidor";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: message }));
    const user = userEvent.setup();
    render(<ServerTable servers={servers} lbOff={true} lb="lb1" onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Quitar web-b" }));
    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("Cancelar closes the dialog and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<ServerTable servers={servers} lbOff={false} lb="lb1" onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Quitar web-a" }));
    await user.click(await screen.findByRole("button", { name: "Cancelar" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
