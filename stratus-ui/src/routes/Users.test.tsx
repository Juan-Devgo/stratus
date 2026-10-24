import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";

import { Users } from "./Users";

// El aviso se comprueba con un espía: Sonner guarda sus avisos en un estado global del módulo.
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

const NO_IP = "La VM no tiene dirección IP; verifique que esté encendida";
const VMS = {
  vms: [{ name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" }],
  list_error: "",
};

// Historial que devuelve el backend falso; una prueba lo reemplaza para simular filas nuevas.
let historyRows: unknown[] = [];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// Responde la lista de VMs y delega la de usuarios en usersResponse.
function mockApi(usersResponse: () => Response) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url === "/api/prepare/vms") {
      return jsonResponse(VMS);
    }
    if (url === "/api/users/history") {
      return jsonResponse({ operations: historyRows });
    }
    if (url === "/api/users/vms/vm1/users") {
      return usersResponse();
    }
    throw new Error(`fetch inesperado: ${url}`);
  });
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

describe("Users", () => {
  it("AC-2 no VM selected: empty list and disabled create", async () => {
    const fetchSpy = mockApi(() => jsonResponse({ users: [] }));
    render(<Users />);

    expect(await screen.findByText("Sin usuarios regulares")).toBeInTheDocument();
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith("/api/prepare/vms", expect.anything()));
    expect(fetchSpy.mock.calls.some(([url]) => String(url).startsWith("/api/users/vms"))).toBe(false);
    expect(screen.getByRole("button", { name: "Crear usuario" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-3 shows the no-IP message when the list fails", async () => {
    mockApi(() => jsonResponse({ error: NO_IP }, 409));
    const user = userEvent.setup();
    render(<Users />);

    await selectVm1(user);

    expect(await screen.findByText("Usuarios no disponibles")).toBeInTheDocument();
    expect(screen.getByText(NO_IP)).toBeInTheDocument();
  });

  it("AC-34 toast on a successful list, message on failure", async () => {
    let fail = false;
    mockApi(() =>
      fail
        ? jsonResponse({ error: "Error interno, revise el registro del servidor" }, 500)
        : jsonResponse({ users: [{ username: "ana", uid: 1001, sudo: true, ssh_key: true }] }),
    );
    const user = userEvent.setup();
    render(<Users />);

    await selectVm1(user);

    expect(await screen.findByText("ana")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(toast.success).toHaveBeenCalledWith("Lista de usuarios actualizada");

    // Con otra VM la lista falla: aparece el Alert y no hay otro aviso.
    fail = true;
    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));
    await user.click(await screen.findByRole("option", { name: "Seleccione una VM" }));
    await selectVm1(user);

    expect(await screen.findByText("Error interno, revise el registro del servidor")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("AC-34 toast again after a create", async () => {
    const created = { message: "Usuario luis creado en vm1; inicia sesión con su contraseña en la consola o con su - luis", key_available: false };
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/users/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/users/vms/vm1/users" && init?.method === "POST") {
        return jsonResponse(created, 201);
      }
      if (url === "/api/users/vms/vm1/users") {
        return jsonResponse({ users: [{ username: "ana", uid: 1001, sudo: true, ssh_key: true }] });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup();
    render(<Users />);
    await selectVm1(user);
    await screen.findByText("ana");
    expect(toast.success).toHaveBeenCalledTimes(1);

    await user.type(screen.getByLabelText("Nombre de usuario"), "luis");
    await user.type(screen.getByLabelText("Contraseña"), "Secreta123");
    await user.type(screen.getByLabelText("Confirmar contraseña"), "Secreta123");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText(created.message)).toBeInTheDocument();
    // Tras crear se vuelve a leer la lista y aparece el aviso otra vez.
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2));
    const listCalls = fetchSpy.mock.calls.filter(
      ([url, init]) => String(url) === "/api/users/vms/vm1/users" && init?.method === "GET",
    );
    expect(listCalls).toHaveLength(2);
  });

  it("AC-34 toast again after a delete", async () => {
    let users = [
      { username: "ana", uid: 1001, sudo: true, ssh_key: true },
      { username: "luis", uid: 1002, sudo: false, ssh_key: false },
    ];
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/users/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/users/vms/vm1/users/luis?remove_files=false" && init?.method === "DELETE") {
        users = users.filter((item) => item.username !== "luis");
        return jsonResponse({ message: "Usuario luis eliminado de vm1" });
      }
      if (url === "/api/users/vms/vm1/users") {
        return jsonResponse({ users });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup();
    render(<Users />);
    await selectVm1(user);
    await screen.findByText("luis");
    expect(toast.success).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Eliminar luis" }));
    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Usuario luis eliminado de vm1")).toBeInTheDocument();
    // Tras eliminar se vuelve a leer la lista: luis ya no aparece y el aviso sale otra vez.
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole("cell", { name: "luis" })).not.toBeInTheDocument());
    const listCalls = fetchSpy.mock.calls.filter(
      ([url, init]) => String(url) === "/api/users/vms/vm1/users" && init?.method === "GET",
    );
    expect(listCalls).toHaveLength(2);
  });

  it("AC-26 shows the module history", async () => {
    historyRows = [
      {
        id: 2,
        operation: "delete_user",
        vm: "vm1",
        command: "users/delete-user.sh",
        args: ["vm1", "luis", "no"],
        exit_code: 5,
        stderr: "No se pudieron terminar los procesos de luis",
        result: "error",
        created_at: "2026-10-22T15:00:00Z",
      },
      {
        id: 1,
        operation: "create_user",
        vm: "vm1",
        command: "users/create-user.sh",
        args: ["vm1", "ana", "yes", "yes"],
        exit_code: 0,
        stderr: "",
        result: "ok",
        created_at: "2026-10-22T14:00:00Z",
      },
    ];
    mockApi(() => jsonResponse({ users: [] }));
    const user = userEvent.setup();
    render(<Users />);

    expect(await screen.findByText("delete_user")).toBeInTheDocument();
    expect(screen.getByText("create_user")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "Ver stderr" });
    expect(buttons).toHaveLength(2);

    await user.click(buttons[0]);

    expect(await screen.findByText("No se pudieron terminar los procesos de luis")).toBeInTheDocument();
  });

  it("AC-26 shows an alert when the history cannot be loaded", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/users/history") {
        return jsonResponse({ error: "Error interno, revise el registro del servidor" }, 500);
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    render(<Users />);

    expect(await screen.findByText("Historial no disponible")).toBeInTheDocument();
    expect(screen.getByText("Error interno, revise el registro del servidor")).toBeInTheDocument();
  });

  it("AC-26 reloads the history after an operation", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/prepare/vms") {
        return jsonResponse(VMS);
      }
      if (url === "/api/users/history") {
        return jsonResponse({ operations: historyRows });
      }
      if (url === "/api/users/vms/vm1/users" && init?.method === "POST") {
        historyRows = [
          {
            id: 1,
            operation: "create_user",
            vm: "vm1",
            command: "users/create-user.sh",
            args: ["vm1", "luis", "no", "no"],
            exit_code: 0,
            stderr: "",
            result: "ok",
            created_at: "2026-10-22T14:00:00Z",
          },
        ];
        return jsonResponse({ message: "Usuario luis creado en vm1", key_available: false }, 201);
      }
      if (url === "/api/users/vms/vm1/users") {
        return jsonResponse({ users: [] });
      }
      throw new Error(`fetch inesperado: ${url}`);
    });
    const user = userEvent.setup();
    render(<Users />);
    await selectVm1(user);
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith("/api/users/history", expect.anything()));
    expect(screen.queryByText("create_user")).not.toBeInTheDocument();

    await user.type(screen.getByLabelText("Nombre de usuario"), "luis");
    await user.type(screen.getByLabelText("Contraseña"), "Secreta123");
    await user.type(screen.getByLabelText("Confirmar contraseña"), "Secreta123");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    // La fila nueva aparece sin recargar la página.
    expect(await screen.findByText("create_user")).toBeInTheDocument();
  });
});
