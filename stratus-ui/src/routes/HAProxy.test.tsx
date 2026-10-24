import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";

import { HAProxy } from "./HAProxy";

// El aviso se comprueba con un espía: Sonner guarda sus avisos en un estado global del módulo.
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

const LB1_ON = { name: "lb1", state: "running", ip: "192.168.1.60", subnet: "10.10.1.0/24", frontend_port: 80, server_count: 2 };
const LB1_OFF = { ...LB1_ON, state: "poweroff", ip: "" };
const SERVERS = [
  { name: "server1", vm: "web-a", ip: "10.10.1.11", port: 80, state: "-" },
  { name: "server2", vm: "web-b", ip: "10.10.1.12", port: 8080, state: "-" },
];
const VMS = [
  { name: "lb1", state: "running", ip: "192.168.1.60", user_key_generated: false, role: "lb", lb: "", lb_ip: "" },
  { name: "web-a", state: "running", ip: "10.10.1.11", user_key_generated: false, role: "server", lb: "lb1", lb_ip: "192.168.1.60" },
  { name: "web-c", state: "running", ip: "192.168.1.70", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
];

// Respuestas del backend falso; cada prueba cambia lo que necesita.
let lbs: unknown[] = [];
let historyRows: unknown[] = [];
let statusResponse: () => Response = () => jsonResponse({ states: {} });
let calls: string[] = [];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mockApi() {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    calls.push(url);
    if (url === "/api/haproxy/lbs") {
      return jsonResponse({ lbs, list_error: "" });
    }
    if (url === "/api/prepare/vms") {
      return jsonResponse({ vms: VMS, list_error: "" });
    }
    if (url === "/api/haproxy/history") {
      return jsonResponse({ operations: historyRows });
    }
    if (url === "/api/haproxy/lbs/lb1/servers") {
      return jsonResponse({ servers: SERVERS });
    }
    if (url === "/api/haproxy/lbs/lb1/status") {
      return statusResponse();
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
}

async function selectLb1(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "Seleccionar lb1" }));
}

beforeEach(() => {
  lbs = [LB1_ON];
  historyRows = [];
  statusResponse = () => jsonResponse({ states: { server1: "UP", server2: "DOWN" } });
  calls = [];
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(toast.success).mockClear();
});

describe("HAProxy", () => {
  it("AC-9 lists the load balancers and asks to select one before anything else", async () => {
    mockApi();
    render(<HAProxy />);

    const row = (await screen.findByText("192.168.1.60")).closest("tr")!;
    for (const text of ["lb1", "Encendida", "10.10.1.0/24", "80", "2"]) {
      expect(within(row).getByText(text)).toBeInTheDocument();
    }
    // Sin balanceador seleccionado no se piden servidores ni estado.
    expect(calls).not.toContain("/api/haproxy/lbs/lb1/servers");
    expect(screen.getAllByText("Seleccione un balanceador para continuar").length).toBeGreaterThan(0);
  });

  it("AC-25 table shows UP and DOWN with the toast when the load balancer is running", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<HAProxy />);

    await selectLb1(user);

    const first = (await screen.findByText("server1")).closest("tr")!;
    expect(within(first).getByText("web-a")).toBeInTheDocument();
    expect(within(first).getByText("10.10.1.11")).toBeInTheDocument();
    expect(await within(first).findByText("UP")).toBeInTheDocument();
    const second = screen.getByText("server2").closest("tr")!;
    expect(within(second).getByText("DOWN")).toBeInTheDocument();
    expect(within(second).getByText("8080")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Estado del balanceador actualizado");
    expect(calls).toContain("/api/haproxy/lbs/lb1/status");
    expect(screen.queryByText(/El balanceador está apagado/)).not.toBeInTheDocument();
  });

  it("AC-46 an off load balancer shows the stored servers with state -, the note, no status call, no toast and no error", async () => {
    lbs = [LB1_OFF];
    mockApi();
    const user = userEvent.setup();
    render(<HAProxy />);

    await selectLb1(user);

    const first = (await screen.findByText("server1")).closest("tr")!;
    expect(within(first).getByText("10.10.1.11")).toBeInTheDocument();
    expect(within(first).getByText("-")).toBeInTheDocument();
    expect(screen.getByText("El balanceador está apagado; los datos son los últimos conocidos")).toBeInTheDocument();
    expect(calls).not.toContain("/api/haproxy/lbs/lb1/status");
    expect(toast.success).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("AC-25 a failed status read shows an error and keeps the servers with state -", async () => {
    statusResponse = () => jsonResponse({ error: "No se pudo leer el estado de HAProxy" }, 500);
    mockApi();
    const user = userEvent.setup();
    render(<HAProxy />);

    await selectLb1(user);

    expect(await screen.findByText("No se pudo leer el estado de HAProxy")).toBeInTheDocument();
    expect(screen.getByText("server1")).toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("AC-35 shows the module history with Ver stderr", async () => {
    historyRows = [
      {
        id: 2,
        operation: "refresh_lb",
        vm: "lb1",
        command: "haproxy/refresh-lb.sh",
        args: ["lb1", "1", "80"],
        exit_code: 10,
        stderr: "Configuración de HAProxy inválida",
        result: "error",
        created_at: "2026-10-24T15:00:00Z",
      },
      {
        id: 1,
        operation: "create_lb",
        vm: "lb1",
        command: "haproxy/create-lb.sh",
        args: ["lb1", "1024", "1"],
        exit_code: 0,
        stderr: "",
        result: "ok",
        created_at: "2026-10-24T14:00:00Z",
      },
    ];
    mockApi();
    const user = userEvent.setup();
    render(<HAProxy />);

    expect(await screen.findByText("refresh_lb")).toBeInTheDocument();
    expect(screen.getByText("create_lb")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "Ver stderr" });
    expect(buttons).toHaveLength(2);
    await user.click(buttons[0]);
    expect(await screen.findByText("Configuración de HAProxy inválida")).toBeInTheDocument();
  });

  it("AC-13 the assign form offers only plain VMs", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<HAProxy />);

    await selectLb1(user);
    await user.click(await screen.findByRole("combobox", { name: "VM a asignar" }));

    expect(await screen.findByRole("option", { name: "web-c" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "web-a" })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "lb1" })).not.toBeInTheDocument();
  });

  it("AC-22 Actualizar IPs reloads the lists afterwards", async () => {
    const fetchSpy = mockApi();
    fetchSpy.mockImplementation(async (input, init) => {
      const url = String(input);
      calls.push(`${init?.method ?? "GET"} ${url}`);
      if (url === "/api/haproxy/lbs/lb1/refresh") {
        return jsonResponse({ message: "IPs de lb1 actualizadas y HAProxy recargado" });
      }
      if (url === "/api/haproxy/lbs") {
        return jsonResponse({ lbs, list_error: "" });
      }
      if (url === "/api/prepare/vms") {
        return jsonResponse({ vms: VMS, list_error: "" });
      }
      if (url === "/api/haproxy/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/haproxy/lbs/lb1/servers") {
        return jsonResponse({ servers: SERVERS });
      }
      return statusResponse();
    });
    const user = userEvent.setup();
    render(<HAProxy />);
    await selectLb1(user);
    await screen.findByText("server1");

    await user.click(screen.getByRole("button", { name: "Actualizar IPs" }));

    expect(await screen.findByText("IPs de lb1 actualizadas y HAProxy recargado")).toBeInTheDocument();
    await waitFor(() => expect(calls.filter((call) => call === "GET /api/haproxy/lbs")).toHaveLength(2));
    expect(calls.filter((call) => call === "GET /api/haproxy/history")).toHaveLength(2);
  });

  it("shows the empty state and the list error", async () => {
    lbs = [];
    mockApi();
    render(<HAProxy />);

    expect(await screen.findByText("Sin balanceadores")).toBeInTheDocument();
  });
});
