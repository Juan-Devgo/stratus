import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";

import { App } from "../App";
import { Prepare } from "./Prepare";

const LIST_ERROR = "No se pudo consultar el estado de las VMs en VirtualBox (código 99): Operación no implementada";
const CATALOG = ["openssh-server", "haproxy", "nginx", "apache2", "curl", "htop", "git", "ufw"];

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

// Historial que devuelve el backend falso; el cambio de hostname le agrega su fila.
let historyRows: unknown[] = [];

// Responde cada endpoint de carga de la pantalla con su propio cuerpo.
function mockApi(vmsBody: unknown) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url === "/api/prepare/catalog") {
      return jsonResponse({ packages: CATALOG });
    }
    if (url === "/api/prepare/vms") {
      return jsonResponse(vmsBody);
    }
    if (url === "/api/prepare/history") {
      return jsonResponse({ operations: historyRows });
    }
    if (url === "/api/prepare/vms/vm1/hostname") {
      historyRows = [
        {
          id: 1,
          operation: "set_hostname",
          vm: "vm1",
          command: "prepare/set-hostname.sh",
          args: ["vm1", "ssh", "2201", "web-01"],
          exit_code: 0,
          stderr: "",
          result: "ok",
          created_at: "2026-10-21T15:00:00Z",
        },
      ];
      return jsonResponse({ message: "Hostname de vm1 cambiado a web-01" });
    }
    if (url === "/api/prepare/vms/vm1/issue") {
      return jsonResponse({ message: "/etc/issue de vm1 actualizado" });
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
}

// Formulario que contiene el control, para buscar dentro de él con within().
function formOf(element: HTMLElement): HTMLElement {
  const form = element.closest("form");
  if (!form) {
    throw new Error("el control no está dentro de un formulario");
  }
  return form;
}

beforeEach(() => {
  historyRows = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Prepare", () => {
  it("AC-3 shows SQLite VMs when listing fails", async () => {
    const body = {
      vms: [
        { name: "vm1", state: "unknown", ip: "", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
        { name: "vm2", state: "unknown", ip: "", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
      ],
      list_error: LIST_ERROR,
    };
    const fetchSpy = mockApi(body);
    const user = userEvent.setup();
    render(<Prepare />);

    expect(await screen.findByText(LIST_ERROR)).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith("/api/prepare/vms", expect.objectContaining({ method: "GET" }));

    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    expect(await screen.findByRole("option", { name: "vm1 - Desconocido - -" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "vm2 - Desconocido - -" })).toBeInTheDocument();
  });

  it("AC-26 create card describes the base-disk clone", async () => {
    mockApi({ vms: [], list_error: "" });
    render(<Prepare />);

    const description = await screen.findByText(/Crea un clon de Debian 13 sobre el disco base compartido/);
    expect(description).toHaveTextContent("disco diferencial, red en puente, IP por DHCP");
    expect(description).toHaveTextContent("Puede tardar varios minutos");
    expect(screen.queryByText(/Instalación desatendida/)).not.toBeInTheDocument();
  });

  it("AC-23 the Preparar sistema card opens the module screen", async () => {
    mockApi({ vms: [], list_error: "" });
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole("link", { name: /Preparar sistema/ }));

    expect(await screen.findByRole("heading", { level: 1, name: "Preparar sistema" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "VM de trabajo" })).toBeInTheDocument();
  });

  it("AC-6 loads the catalog into the package form", async () => {
    const fetchSpy = mockApi({ vms: [], list_error: "" });
    const user = userEvent.setup();
    render(<Prepare />);

    await vi.waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith("/api/prepare/catalog", expect.objectContaining({ method: "GET" })),
    );
    await user.click(screen.getByRole("combobox", { name: "Catálogo" }));
    for (const pkg of CATALOG) {
      expect(await screen.findByRole("option", { name: pkg })).toBeInTheDocument();
    }
    expect(screen.getByRole("button", { name: "Instalar" })).toBeDisabled();
  });

  it("AC-17 sets the hostname of the selected VM", async () => {
    const body = {
      vms: [{ name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" }],
      list_error: "",
    };
    const fetchSpy = mockApi(body);
    const user = userEvent.setup();
    render(<Prepare />);

    // Varios formularios tienen un botón "Aplicar": se usa el del formulario de hostname.
    const form = within(formOf(screen.getByLabelText("Hostname")));
    expect(form.getByRole("button", { name: "Aplicar" })).toBeDisabled();
    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    await user.click(await screen.findByRole("option", { name: "vm1 - Encendida - 192.168.1.50" }));
    await user.type(screen.getByLabelText("Hostname"), "web-01");
    await user.click(form.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText("Hostname de vm1 cambiado a web-01")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/prepare/vms/vm1/hostname",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ hostname: "web-01" }) }),
    );
  });

  it("AC-14 applies /etc/issue on the selected VM", async () => {
    const body = {
      vms: [{ name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" }],
      list_error: "",
    };
    const fetchSpy = mockApi(body);
    const user = userEvent.setup();
    render(<Prepare />);

    const form = within(formOf(screen.getByLabelText("Mensaje")));
    expect(form.getByRole("button", { name: "Aplicar" })).toBeDisabled();
    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    await user.click(await screen.findByRole("option", { name: "vm1 - Encendida - 192.168.1.50" }));
    await user.type(screen.getByLabelText("Mensaje"), "Hola");
    await user.click(form.getByRole("checkbox", { name: "Host" }));
    await user.click(form.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText("/etc/issue de vm1 actualizado")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/prepare/vms/vm1/issue",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ message: "Hola", fields: ["hostname"] }) }),
    );
  });

  it("AC-22 shows the history and reloads it after an operation", async () => {
    const body = {
      vms: [{ name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" }],
      list_error: "",
    };
    const fetchSpy = mockApi(body);
    const user = userEvent.setup();
    render(<Prepare />);

    expect(await screen.findByText("Sin operaciones registradas")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith("/api/prepare/history", expect.objectContaining({ method: "GET" }));

    const form = within(formOf(screen.getByLabelText("Hostname")));
    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    await user.click(await screen.findByRole("option", { name: "vm1 - Encendida - 192.168.1.50" }));
    await user.type(screen.getByLabelText("Hostname"), "web-01");
    await user.click(form.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText("set_hostname")).toBeInTheDocument();
    expect(screen.queryByText("Sin operaciones registradas")).not.toBeInTheDocument();
  });
});
