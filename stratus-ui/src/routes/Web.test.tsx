import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { Web } from "./Web";

const WEB_A = { name: "web-a", type: "nginx", state: "running", ip: "192.168.1.40", fqdn: "", lb: "" };
const HISTORY = [
  {
    id: 2,
    operation: "start_web",
    vm: "web-a",
    command: "web/start-web.sh",
    args: ["web-a", "nginx"],
    exit_code: 0,
    stderr: "",
    result: "ok",
    created_at: "2026-11-01T10:05:00Z",
  },
  {
    id: 1,
    operation: "install_page",
    vm: "web-a",
    command: "web/install-page.sh",
    args: ["web-a", "/tmp/page.tar.gz"],
    exit_code: 11,
    stderr: "El archivo no tiene index.html en la raíz",
    result: "error",
    created_at: "2026-11-01T10:00:00Z",
  },
];

// Respuestas del backend falso; cada prueba cambia lo que necesita.
let listError = "";
let calls: string[] = [];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mockApi() {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    calls.push(url);
    if (url === "/api/web/servers") {
      return jsonResponse({ servers: [WEB_A], list_error: listError });
    }
    if (url === "/api/web/history") {
      return jsonResponse({ operations: HISTORY });
    }
    if (url === "/api/web/servers/web-a/start") {
      return jsonResponse({ message: "nginx iniciado en web-a", active_state: "failed" });
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
}

beforeEach(() => {
  listError = "";
  calls = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Web", () => {
  it("AC-21 start that ends failed shows the success and the warning to check Estado", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<Web />);

    const group = await screen.findByRole("group", { name: "Acciones de web-a" });
    await user.click(within(group).getByRole("button", { name: "Iniciar" }));

    expect(await screen.findByText("nginx iniciado en web-a")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    expect(screen.getByText("El servicio nginx quedó en estado failed; revise Estado")).toBeInTheDocument();
    // Tras la operación se recargan la lista y el historial.
    await waitFor(() => expect(calls.filter((url) => url === "/api/web/servers")).toHaveLength(2));
    expect(calls.filter((url) => url === "/api/web/history")).toHaveLength(2);
  });

  it("AC-53 lists the module history with Ver stderr per row", async () => {
    mockApi();
    const user = userEvent.setup();
    render(<Web />);

    expect(await screen.findByText("install_page")).toBeInTheDocument();
    expect(screen.getByText("start_web")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "Ver stderr" });
    expect(buttons).toHaveLength(2);

    await user.click(buttons[1]);
    expect(await screen.findByText("El archivo no tiene index.html en la raíz")).toBeInTheDocument();
  });

  it("AC-8 shows the list error from the backend", async () => {
    listError = "No se pudo leer el estado de las VMs";
    mockApi();
    render(<Web />);

    expect(await screen.findByText("No se pudo leer el estado de las VMs")).toBeInTheDocument();
  });
});
