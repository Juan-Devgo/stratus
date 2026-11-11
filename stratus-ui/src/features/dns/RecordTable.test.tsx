import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { RecordTable } from "./RecordTable";
import type { DnsVm } from "./types";

const VM1: DnsVm = {
  name: "vm1",
  kind: "",
  role: "vm",
  state: "running",
  ip: "192.168.1.31",
  fqdn: "",
  record_ip: "",
};
const LB1: DnsVm = { ...VM1, name: "lb1", role: "lb", ip: "192.168.1.60" };
const VM2: DnsVm = { ...VM1, name: "vm2", state: "poweroff", ip: "" };
const WEB_A: DnsVm = {
  ...VM1,
  name: "web-a",
  kind: "nginx",
  ip: "192.168.1.40",
  fqdn: "web-a.stratus.lan",
  record_ip: "192.168.1.40",
};

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function rowOf(name: string): HTMLElement {
  return screen.getByRole("cell", { name }).closest("tr") as HTMLElement;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("RecordTable", () => {
  it("AC-30 adds the selected VMs and shows the result of each one", async () => {
    const results = [
      { vm: "lb1", ok: true, message: "Registro lb1.stratus.lan agregado" },
      { vm: "vm1", ok: true, message: "Registro vm1.stratus.lan agregado" },
      { vm: "vm2", ok: false, message: "La VM no tiene dirección IP; verifique que esté encendida" },
    ];
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Registros agregados: 2 de 3", results }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<RecordTable vms={[LB1, VM1, VM2, WEB_A]} busy={false} onDone={onDone} />);

    // Una VM con registro no se puede elegir.
    expect(within(rowOf("web-a")).queryByRole("checkbox")).not.toBeInTheDocument();
    const add = screen.getByRole("button", { name: "Agregar registros" });
    expect(add).toBeDisabled();

    for (const name of ["vm1", "lb1", "vm2"]) {
      await user.click(screen.getByRole("checkbox", { name: `Seleccionar ${name}` }));
    }
    await user.click(add);

    expect(await screen.findByText("Registros agregados: 2 de 3")).toBeInTheDocument();
    const list = screen.getByRole("list", { name: "Resultado por VM" });
    expect(within(list).getByText(/vm2: La VM no tiene dirección IP; verifique que esté encendida/)).toBeInTheDocument();
    expect(within(list).getByText(/vm1: Registro vm1.stratus.lan agregado/)).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/dns/records");
    expect(init?.method).toBe("POST");
    // En el orden de la tabla.
    expect(JSON.parse(init?.body as string)).toEqual({ vms: ["lb1", "vm1", "vm2"] });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-31 no selection for a server of a load balancer", () => {
    const server: DnsVm = { ...VM1, name: "web-b", kind: "apache", role: "server", ip: "10.10.1.11" };
    render(<RecordTable vms={[WEB_A, LB1, server, VM1]} busy={false} onDone={vi.fn()} />);

    expect(within(rowOf("web-b")).getByText("Servidor web (Apache)")).toBeInTheDocument();
    expect(within(rowOf("web-b")).queryByRole("checkbox")).not.toBeInTheDocument();
    // Las demás VMs sin registro siguen pudiéndose elegir.
    expect(screen.getByRole("checkbox", { name: "Seleccionar lb1" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Seleccionar vm1" })).toBeInTheDocument();
    expect(within(rowOf("web-a")).getByText("web-a.stratus.lan")).toBeInTheDocument();
    expect(within(rowOf("lb1")).getByText("Balanceador")).toBeInTheDocument();
  });

  it("AC-58 hidden record has no Quitar registro", () => {
    const server: DnsVm = { ...VM1, name: "web-b", kind: "apache", role: "server", ip: "10.10.1.11" };
    render(<RecordTable vms={[server, WEB_A]} busy={false} onDone={vi.fn()} />);

    const row = within(rowOf("web-b"));
    expect(row.queryByRole("button", { name: "Quitar registro" })).not.toBeInTheDocument();
    expect(row.queryByText(/stratus\.lan/)).not.toBeInTheDocument();
    expect(within(rowOf("web-a")).getByRole("button", { name: "Quitar registro" })).toBeInTheDocument();
  });

  it("AC-33 Quitar registro asks first and then deletes the record", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Registro de web-a quitado" }));
    const user = userEvent.setup();
    render(<RecordTable vms={[WEB_A, VM1]} busy={false} onDone={vi.fn()} />);

    expect(within(rowOf("vm1")).queryByRole("button", { name: "Quitar registro" })).not.toBeInTheDocument();
    await user.click(within(rowOf("web-a")).getByRole("button", { name: "Quitar registro" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("¿Quitar el registro de web-a?")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Registro de web-a quitado")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/dns/records/web-a");
    expect(init?.method).toBe("DELETE");
  });

  it("AC-23 shows no resolver column or switch", () => {
    render(<RecordTable vms={[VM1, WEB_A]} busy={false} onDone={vi.fn()} />);

    expect(screen.queryByRole("columnheader", { name: "Resolvedor" })).not.toBeInTheDocument();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });
});
