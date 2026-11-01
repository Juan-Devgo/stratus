import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { WebServer } from "./types";
import { WebTable } from "./WebTable";

const SERVERS: WebServer[] = [
  { name: "web-a", type: "nginx", state: "running", ip: "192.168.1.40", fqdn: "web-a.stratus.lan", lb: "" },
  { name: "web-b", type: "apache", state: "poweroff", ip: "", fqdn: "", lb: "lb1" },
];

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// Celdas de datos de la fila del servidor, en el orden de las columnas.
function cellsOf(name: string): string[] {
  const row = screen.getByRole("cell", { name }).closest("tr") as HTMLElement;
  return within(row)
    .getAllByRole("cell")
    .map((cell) => cell.textContent ?? "");
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("WebTable", () => {
  it("AC-8 lists each server with its type, state, IP, FQDN and load balancer", () => {
    render(<WebTable servers={SERVERS} busy={false} onDone={vi.fn()} />);

    for (const header of ["Nombre", "Tipo", "Estado", "IP", "FQDN", "Balanceador"]) {
      expect(screen.getByRole("columnheader", { name: header })).toBeInTheDocument();
    }
    expect(cellsOf("web-a")).toEqual(["web-a", "Nginx", "Encendida", "192.168.1.40", "web-a.stratus.lan", "-"]);
    expect(cellsOf("web-b")).toEqual(["web-b", "Apache", "Apagada", "-", "-", "lb1"]);
    expect(screen.getByRole("group", { name: "Acciones de web-a" })).toBeInTheDocument();
  });

  it("AC-8 shows an empty state without servers", () => {
    render(<WebTable servers={[]} busy={false} onDone={vi.fn()} />);
    expect(screen.getByText("Sin servidores web")).toBeInTheDocument();
  });

  it("AC-10 delete names the VM, waits for Confirmar and shows the backend text", async () => {
    const text = "La VM es servidor de un balanceador; quítela primero";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: text }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<WebTable servers={SERVERS} busy={false} onDone={onDone} />);

    await user.click(within(screen.getByRole("group", { name: "Acciones de web-b" })).getByRole("button", { name: "Eliminar" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("¿Eliminar el servidor web web-b?")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/web/servers/web-b");
    expect(init?.method).toBe("DELETE");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-11 Instalar página opens the dialog for that server", async () => {
    const user = userEvent.setup();
    render(<WebTable servers={SERVERS} busy={false} onDone={vi.fn()} />);

    await user.click(
      within(screen.getByRole("group", { name: "Acciones de web-a" })).getByRole("button", { name: "Instalar página" }),
    );

    expect(await screen.findByText("Instalar página en web-a")).toBeInTheDocument();
  });

  it("REQ-38 busy disables the row buttons", () => {
    render(<WebTable servers={SERVERS} busy={true} onDone={vi.fn()} />);
    const group = screen.getByRole("group", { name: "Acciones de web-a" });
    for (const button of within(group).getAllByRole("button")) {
      expect(button).toBeDisabled();
    }
  });
});
