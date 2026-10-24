import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";

import { Services } from "./Services";

// El aviso se comprueba con un espía: Sonner guarda sus avisos en un estado global del módulo.
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

const NO_IP = "La VM no tiene dirección IP; verifique que esté encendida";
const VMS = {
  vms: [{ name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" }],
  list_error: "",
};
const SERVICES = [
  {
    name: "demian-lee",
    description: "Servidor de producción",
    active_state: "active",
    enabled_state: "enabled",
    restart: "on-failure",
    restart_sec: 5,
    executable: "demian-lee",
    args: "",
    env_keys: ["PORT"],
  },
];

// Historial que devuelve el backend falso; una prueba lo reemplaza para simular filas nuevas.
let historyRows: unknown[] = [];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// Responde la lista de VMs y delega la de servicios en servicesResponse.
function mockApi(servicesResponse: () => Response) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url === "/api/prepare/vms") {
      return jsonResponse(VMS);
    }
    if (url === "/api/services/history") {
      return jsonResponse({ operations: historyRows });
    }
    if (url === "/api/services/vms/vm1/services") {
      return servicesResponse();
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
}

// Pegar es mucho más rápido que escribir carácter por carácter y deja el mismo valor en el campo.
async function fill(user: ReturnType<typeof userEvent.setup>, element: HTMLElement, text: string) {
  await user.click(element);
  await user.paste(text);
}

async function selectVm1(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("combobox", { name: "VM de trabajo" }));
  await user.click(await screen.findByRole("option", { name: /vm1/ }));
}

beforeEach(() => {
  historyRows = [];
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(toast.success).mockClear();
});

// Responde las VMs, la lista (con un servicio) y delega la acción de systemctl en actionResponse y
// el estado o los logs en readResponse.
function mockWithAction(
  actionResponse: (url: string) => Promise<Response> | Response,
  readResponse: (url: string) => Promise<Response> | Response = () => jsonResponse({ output: "" }),
) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (url === "/api/prepare/vms") {
      return jsonResponse(VMS);
    }
    if (url === "/api/services/history") {
      return jsonResponse({ operations: historyRows });
    }
    if (url.endsWith("/status") || url.endsWith("/logs")) {
      return readResponse(url);
    }
    if (url === "/api/services/vms/vm1/services") {
      return jsonResponse({ services: SERVICES });
    }
    if (init?.method === "POST" && url.startsWith("/api/services/vms/vm1/services/")) {
      return actionResponse(url);
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
}

describe("Services", () => {
  it("AC-37 row buttons disabled while an operation runs", async () => {
    let resolve: (response: Response) => void = () => undefined;
    mockWithAction(() => new Promise((r) => (resolve = r)));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("demian-lee");

    await user.click(screen.getByRole("button", { name: "Iniciar demian-lee" }));

    expect(await screen.findByLabelText("Cargando")).toBeInTheDocument();
    for (const label of ["Iniciar", "Detener", "Reiniciar", "Habilitar", "Deshabilitar"]) {
      expect(screen.getByRole("button", { name: `${label} demian-lee` })).toBeDisabled();
    }
    // El formulario de creación también espera a que termine la operación.
    expect(screen.getByRole("button", { name: "Crear servicio" })).toBeDisabled();

    resolve(jsonResponse({ message: "Servicio demian-lee iniciado en vm1", active_state: "active" }));
    expect(await screen.findByText("Servicio demian-lee iniciado en vm1")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Iniciar demian-lee" })).toBeEnabled());
    expect(screen.queryByLabelText("Cargando")).toBeNull();
  });

  it("AC-42 shows the warning after Iniciar and reloads the list", async () => {
    const fetchSpy = mockWithAction(() =>
      jsonResponse({ message: "Servicio demian-lee iniciado en vm1", active_state: "failed" }),
    );
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("demian-lee");

    await user.click(screen.getByRole("button", { name: "Iniciar demian-lee" }));

    expect(await screen.findByText("Servicio demian-lee iniciado en vm1")).toBeInTheDocument();
    expect(screen.getByText(/El servicio demian-lee quedó en estado failed; revise Estado y Logs/)).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith("/api/services/vms/vm1/services/demian-lee/start", expect.objectContaining({ method: "POST" }));
    // La lista se vuelve a leer después de la acción, con su aviso.
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2));
  });

  it("a failed action shows the failure message without the warning", async () => {
    mockWithAction(() => jsonResponse({ error: "Solo se pueden gestionar servicios creados por Stratus" }, 409));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("demian-lee");

    await user.click(screen.getByRole("button", { name: "Detener demian-lee" }));

    expect(await screen.findByText("Solo se pueden gestionar servicios creados por Stratus")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    expect(screen.queryByText(/revise Estado y Logs/)).toBeNull();
  });

  it("AC-1 a created service reloads the list with its toast", async () => {
    const created = { message: "Servicio demian-lee creado en vm1", active_state: "active" };
    let listed = false;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/services/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/services/vms/vm1/services" && init?.method === "POST") {
        listed = true; // a partir de aquí la VM tiene el servicio
        return jsonResponse(created, 201);
      }
      if (url === "/api/services/vms/vm1/services") {
        return jsonResponse({ services: listed ? SERVICES : [] });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));

    await fill(user, screen.getByLabelText("Nombre del servicio"), "demian-lee");
    await fill(user, screen.getByLabelText("Descripción"), "Servidor de producción");
    await user.upload(screen.getByLabelText("Archivo de la aplicación (.tar.gz)"), new File(["x"], "app.tar.gz"));
    await fill(user, screen.getByLabelText("Ejecutable"), "demian-lee");
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText(created.message)).toBeInTheDocument();
    expect(await screen.findByRole("cell", { name: "demian-lee" })).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledTimes(2);
    expect(fetchSpy.mock.calls.filter(([url]) => String(url) === "/api/services/vms/vm1/services")).toHaveLength(3);
  });

  it("AC-16 no VM selected: empty list and no request for services", async () => {
    const fetchSpy = mockApi(() => jsonResponse({ services: [] }));
    render(<Services />);

    expect(await screen.findByText("Sin servicios de Stratus")).toBeInTheDocument();
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith("/api/prepare/vms", expect.anything()));
    expect(fetchSpy.mock.calls.some(([url]) => String(url).startsWith("/api/services/vms"))).toBe(false);
    expect(toast.success).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Crear servicio" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-17 shows the no-IP message when the list fails", async () => {
    mockApi(() => jsonResponse({ error: NO_IP }, 409));
    const user = userEvent.setup();
    render(<Services />);

    await selectVm1(user);

    expect(await screen.findByText("Servicios no disponibles")).toBeInTheDocument();
    expect(screen.getByText(NO_IP)).toBeInTheDocument();
  });

  it("AC-15 toast on a successful list, message on failure", async () => {
    let fail = false;
    mockApi(() =>
      fail
        ? jsonResponse({ error: "Error interno, revise el registro del servidor" }, 500)
        : jsonResponse({ services: SERVICES }),
    );
    const user = userEvent.setup();
    render(<Services />);

    await selectVm1(user);

    expect(await screen.findByText("demian-lee")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(toast.success).toHaveBeenCalledWith("Lista de servicios actualizada");
    expect(screen.queryByText("Servicios no disponibles")).toBeNull();

    // Con otra VM la lista falla: aparece el Alert y no hay otro aviso.
    fail = true;
    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    await user.click(await screen.findByRole("option", { name: "Seleccione una VM" }));
    await selectVm1(user);

    expect(await screen.findByText("Servicios no disponibles")).toBeInTheDocument();
    expect(screen.getByText("Error interno, revise el registro del servidor")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("AC-21 Estado shows the output panel", async () => {
    const output = "● demian-lee.service - Servidor de producción\n     Active: failed (Result: exit-code)";
    const fetchSpy = mockWithAction(() => jsonResponse({}), () => jsonResponse({ output }));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");

    await user.click(screen.getByRole("button", { name: "Estado demian-lee" }));

    expect(await screen.findByText("Estado de demian-lee")).toBeInTheDocument();
    expect(screen.getByText(/Active: failed/)).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith("/api/services/vms/vm1/services/demian-lee/status", expect.anything());
    // Un servicio fallido se muestra: no es un fallo de la operación.
    expect(screen.queryByText("La operación falló")).toBeNull();
    expect(screen.queryByText("Operación completada")).toBeNull();
  });

  it("AC-22 Logs shows the output panel", async () => {
    const output = "oct 22 10:00:00 vm1 demian-lee[1]: escuchando en :8080";
    const fetchSpy = mockWithAction(() => jsonResponse({}), () => jsonResponse({ output }));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");

    await user.click(screen.getByRole("button", { name: "Logs demian-lee" }));

    expect(await screen.findByText("Logs de demian-lee")).toBeInTheDocument();
    expect(screen.getByText(/escuchando en :8080/)).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith("/api/services/vms/vm1/services/demian-lee/logs", expect.anything());
  });

  it("AC-21 buttons are disabled while the output loads and a failure shows the message", async () => {
    let resolve: (response: Response) => void = () => undefined;
    mockWithAction(() => jsonResponse({}), () => new Promise((r) => (resolve = r)));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");

    await user.click(screen.getByRole("button", { name: "Estado demian-lee" }));

    expect(await screen.findByLabelText("Cargando")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Logs demian-lee" })).toBeDisabled();

    resolve(jsonResponse({ error: "Solo se pueden gestionar servicios creados por Stratus" }, 409));
    expect(await screen.findByText("Solo se pueden gestionar servicios creados por Stratus")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    expect(screen.queryByText("Estado de demian-lee")).toBeNull();
    await waitFor(() => expect(screen.getByRole("button", { name: "Logs demian-lee" })).toBeEnabled());
  });

  it("AC-21 the output panel is cleared when the VM changes", async () => {
    mockWithAction(() => jsonResponse({}), () => jsonResponse({ output: "Active: active (running)" }));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");
    await user.click(screen.getByRole("button", { name: "Estado demian-lee" }));
    expect(await screen.findByText("Estado de demian-lee")).toBeInTheDocument();

    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    await user.click(await screen.findByRole("option", { name: "Seleccione una VM" }));

    await waitFor(() => expect(screen.queryByText("Estado de demian-lee")).toBeNull());
  });

  it("AC-23 Editar opens the dialog with the row values and saving reloads the list", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/services/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/services/vms/vm1/services/demian-lee" && init?.method === "PUT") {
        return jsonResponse({ message: "Servicio demian-lee actualizado en vm1", active_state: "active" });
      }
      if (url === "/api/services/vms/vm1/services") {
        return jsonResponse({ services: SERVICES });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));

    await user.click(screen.getByRole("button", { name: "Editar demian-lee" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Descripción")).toHaveValue("Servidor de producción");
    await user.click(within(dialog).getByRole("button", { name: "Guardar" }));
    expect(await within(dialog).findByText("Servicio demian-lee actualizado en vm1")).toBeInTheDocument();
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2));
    expect(fetchSpy.mock.calls.filter(([url]) => String(url) === "/api/services/vms/vm1/services")).toHaveLength(2);

    await user.click(within(dialog).getByRole("button", { name: "Cerrar" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("AC-38 Redesplegar opens the dialog with the executable of the row", async () => {
    mockWithAction(() => jsonResponse({}));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");

    await user.click(screen.getByRole("button", { name: "Redesplegar demian-lee" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Redesplegar el servicio demian-lee")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Ejecutable")).toHaveValue("demian-lee");
  });

  it("AC-38 a confirmed redeploy reloads the list with its toast", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/services/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/services/vms/vm1/services/demian-lee/redeploy" && init?.method === "POST") {
        return jsonResponse({ message: "Servicio demian-lee redesplegado en vm1", active_state: "active" });
      }
      if (url === "/api/services/vms/vm1/services") {
        return jsonResponse({ services: SERVICES });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));

    await user.click(screen.getByRole("button", { name: "Redesplegar demian-lee" }));
    const dialog = await screen.findByRole("dialog");
    await user.upload(within(dialog).getByLabelText("Archivo de la aplicación (.tar.gz)"), new File(["x"], "nuevo.tar.gz"));
    await user.click(within(dialog).getByRole("button", { name: "Continuar" }));
    await user.click(within(dialog).getByRole("button", { name: "Confirmar" }));

    expect(await within(dialog).findByText("Servicio demian-lee redesplegado en vm1")).toBeInTheDocument();
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2));
    expect(fetchSpy.mock.calls.filter(([url]) => String(url) === "/api/services/vms/vm1/services")).toHaveLength(2);
  });

  it("AC-28 Eliminar asks for confirmation, deletes and the list no longer shows the service", async () => {
    let deleted = false;
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/services/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/services/vms/vm1/services/demian-lee" && init?.method === "DELETE") {
        deleted = true;
        return jsonResponse({ message: "Servicio demian-lee eliminado de vm1" });
      }
      if (url === "/api/services/vms/vm1/services") {
        return jsonResponse({ services: deleted ? [] : SERVICES });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");

    await user.click(screen.getByRole("button", { name: "Eliminar demian-lee" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("demian-lee");
    // El script no corre hasta confirmar.
    expect(fetchSpy.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false);
    await user.click(within(dialog).getByRole("button", { name: "Confirmar" }));

    expect(await within(dialog).findByText("Servicio demian-lee eliminado de vm1")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("cell", { name: "Servidor de producción" })).toBeNull());
    // El diálogo sigue abierto aunque la lista haya quedado vacía.
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Cerrar" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("AC-34 shows the module history", async () => {
    historyRows = [
      {
        id: 2,
        operation: "stop_service",
        vm: "vm1",
        command: "services/stop-service.sh",
        args: ["vm1", "api"],
        exit_code: 5,
        stderr: "Falló el paso systemctl en la VM (código 5)",
        result: "error",
        created_at: "2026-10-22T15:00:00Z",
      },
      {
        id: 1,
        operation: "create_service",
        vm: "vm1",
        command: "services/create-service.sh",
        args: ["vm1", "demian-lee"],
        exit_code: 0,
        stderr: "",
        result: "ok",
        created_at: "2026-10-22T14:00:00Z",
      },
    ];
    mockApi(() => jsonResponse({ services: [] }));
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);

    expect(await screen.findByText("stop_service")).toBeInTheDocument();
    expect(screen.getByText("create_service")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "Ver stderr" });
    expect(buttons).toHaveLength(2);

    await user.click(buttons[0]);

    expect(await screen.findByText("Falló el paso systemctl en la VM (código 5)")).toBeInTheDocument();
  });

  it("AC-34 shows an alert when the history cannot be loaded", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/services/history") {
        return jsonResponse({ error: "Error interno, revise el registro del servidor" }, 500);
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    render(<Services />);

    expect(await screen.findByText("Historial no disponible")).toBeInTheDocument();
    expect(screen.getByText("Error interno, revise el registro del servidor")).toBeInTheDocument();
  });

  it("AC-34 reloads the history after an operation", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/services/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/services/vms/vm1/services/demian-lee/stop" && init?.method === "POST") {
        historyRows = [
          {
            id: 1,
            operation: "stop_service",
            vm: "vm1",
            command: "services/stop-service.sh",
            args: ["vm1", "demian-lee"],
            exit_code: 0,
            stderr: "",
            result: "ok",
            created_at: "2026-10-22T14:00:00Z",
          },
        ];
        return jsonResponse({ message: "Servicio demian-lee detenido en vm1" });
      }
      if (url === "/api/services/vms/vm1/services") {
        return jsonResponse({ services: SERVICES });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup({ applyAccept: false, delay: null });
    render(<Services />);
    await selectVm1(user);
    await screen.findByText("Servidor de producción");
    expect(screen.queryByText("stop_service")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Detener demian-lee" }));

    expect(await screen.findByText("stop_service")).toBeInTheDocument();
    expect(fetchSpy.mock.calls.filter(([url]) => String(url) === "/api/services/history")).toHaveLength(2);
  });
});
