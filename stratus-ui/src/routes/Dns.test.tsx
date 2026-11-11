import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { Dns } from "./Dns";

// El toast de eliminar no importa aquí: Sonner guarda sus avisos en un estado global del módulo.
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

const SERVER = { name: "dns1", state: "running", ip: "192.168.1.53", zone: "stratus.lan" };
const WEB_A = {
  name: "web-a",
  kind: "nginx",
  role: "vm",
  state: "running",
  ip: "192.168.1.40",
  fqdn: "web-a.stratus.lan",
  record_ip: "192.168.1.40",
};
const HISTORY = [
  {
    id: 2,
    operation: "add_records",
    vm: "dns1",
    command: "dns/add-records.sh",
    args: ["dns1", "stratus.lan", "192.168.1.53", "web-a,192.168.1.40"],
    exit_code: 0,
    stderr: "",
    result: "ok",
    created_at: "2026-11-01T10:05:00Z",
  },
  {
    id: 1,
    operation: "create_dns",
    vm: "dns1",
    command: "dns/create-dns.sh",
    args: ["dns1"],
    exit_code: 13,
    stderr: "La IP 192.168.1.53 ya está en uso",
    result: "error",
    created_at: "2026-11-01T10:00:00Z",
  },
];

const ZONE = [
  { type: "NS", name: "stratus.lan", value: "dns1.stratus.lan.", ttl: 60 },
  { type: "A", name: "web-a.stratus.lan", value: "192.168.1.40", ttl: 60 },
];

// Respuestas del backend falso; cada prueba cambia lo que necesita.
let overview: unknown;
let calls: string[] = [];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mockApi() {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    calls.push(`${init?.method ?? "GET"} ${url}`);
    if (url === "/api/dns") {
      return jsonResponse(overview);
    }
    if (url === "/api/dns/history") {
      return jsonResponse({ operations: HISTORY });
    }
    if (url === "/api/dns/server" && init?.method === "DELETE") {
      overview = { server: null, vms: [{ ...WEB_A, fqdn: "", record_ip: "" }], list_error: "" };
      return jsonResponse({ message: "Servidor DNS dns1 eliminado junto con su disco" });
    }
    if (url === "/api/dns/records/refresh" && init?.method === "POST") {
      return jsonResponse({ message: "Registros actualizados" });
    }
    if (url === "/api/dns/zone") {
      return jsonResponse({ records: ZONE });
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
}

beforeEach(() => {
  overview = { server: SERVER, vms: [WEB_A], list_error: "" };
  calls = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Dns", () => {
  it("AC-23 with a DNS server the create form is hidden and the panel and records show", async () => {
    mockApi();
    render(<Dns />);

    expect(await screen.findByRole("group", { name: "Acciones de dns1" })).toBeInTheDocument();
    expect(screen.getByText("stratus.lan")).toBeInTheDocument();
    expect(screen.getByText("web-a.stratus.lan")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Crear servidor DNS" })).not.toBeInTheDocument();
  });

  it("AC-29 after deleting the server the form returns and no records show", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<Dns />);

    const group = await screen.findByRole("group", { name: "Acciones de dns1" });
    await user.click(within(group).getByRole("button", { name: "Eliminar" }));
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByRole("button", { name: "Crear servidor DNS" })).toBeInTheDocument();
    expect(screen.queryByText("web-a.stratus.lan")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Agregar registros" })).not.toBeInTheDocument();
    await waitFor(() => expect(calls.filter((call) => call === "GET /api/dns")).toHaveLength(2));
  });

  it("AC-53 lists the module history with Ver stderr per row", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<Dns />);

    expect(await screen.findByText("create_dns")).toBeInTheDocument();
    expect(screen.getByText("add_records")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "Ver stderr" });
    expect(buttons).toHaveLength(2);

    await user.click(buttons[1]);
    expect(await screen.findByText("La IP 192.168.1.53 ya está en uso")).toBeInTheDocument();
  });

  it("REQ-46 the records card opens on the VMs tab and does not read the zone", async () => {
    mockApi();
    render(<Dns />);

    expect(await screen.findByText("web-a.stratus.lan")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "VMs" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Zona" })).toHaveAttribute("aria-selected", "false");
    expect(calls).not.toContain("GET /api/dns/zone");
  });

  it("AC-66 the Zona tab reads the zone and lists its records", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<Dns />);

    await user.click(await screen.findByRole("tab", { name: "Zona" }));

    expect(await screen.findByText("dns1.stratus.lan.")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "TTL" })).toBeInTheDocument();
    expect(calls.filter((call) => call === "GET /api/dns/zone")).toHaveLength(1);
  });

  it("AC-67 an operation with the Zona tab open reads the zone again", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<Dns />);

    await user.click(await screen.findByRole("tab", { name: "Zona" }));
    expect(await screen.findByText("dns1.stratus.lan.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Actualizar registros" }));

    await waitFor(() => expect(calls.filter((call) => call === "GET /api/dns/zone")).toHaveLength(2));
    expect(calls).toContain("POST /api/dns/records/refresh");
  });

  it("AC-35 shows the list error from the backend", async () => {
    overview = { server: null, vms: [], list_error: "No se pudo leer el estado de las VMs" };
    mockApi();
    render(<Dns />);

    expect(await screen.findByText("No se pudo leer el estado de las VMs")).toBeInTheDocument();
  });
});
