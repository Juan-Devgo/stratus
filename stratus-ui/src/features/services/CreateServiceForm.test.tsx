import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "@/features/prepare/types";

import { CreateServiceForm } from "./CreateServiceForm";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type User = ReturnType<typeof userEvent.setup>;

// Pegar es mucho más rápido que escribir carácter por carácter y deja el mismo valor en el campo.
async function fill(user: User, element: HTMLElement, text: string) {
  await user.click(element);
  await user.paste(text);
}

// El archivo .zip del AC-5 se sube igual: el filtro "accept" del campo no debe esconderlo.
function setup() {
  return userEvent.setup({ applyAccept: false, delay: null });
}

async function fillBasics(user: User, name = "demian-lee", file = new File(["datos"], "demian-lee.tar.gz")) {
  await fill(user, screen.getByLabelText("Nombre del servicio"), name);
  await fill(user, screen.getByLabelText("Descripción"), "Servidor de producción");
  await user.upload(screen.getByLabelText("Archivo de la aplicación (.tar.gz)"), file);
  await fill(user, screen.getByLabelText("Ejecutable"), "demian-lee");
}

async function addVariable(user: User, key: string, value: string) {
  await user.click(screen.getByRole("button", { name: "Agregar variable" }));
  const keys = screen.getAllByLabelText(/^Clave de la variable/);
  const values = screen.getAllByLabelText(/^Valor de la variable/);
  await fill(user, keys[keys.length - 1], key);
  await fill(user, values[values.length - 1], value);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CreateServiceForm", () => {
  it("AC-1 submits each field, payload before archive, and shows success without warning", async () => {
    const message = "Servicio demian-lee creado en vm1";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(201, { message, active_state: "active" }),
    );
    const onDone = vi.fn();
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={onDone} />);

    await fillBasics(user);
    await addVariable(user, "PORT", "8080");
    await addVariable(user, "SERVER_NAME", "Servidor-Produccion-1");
    await addVariable(user, "ADMIN_PASSWORD", "Secreta123");
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/services/vms/vm1/services");
    expect(init?.method).toBe("POST");
    const form = init?.body as FormData;
    expect([...form.keys()]).toEqual(["payload", "archive"]);
    expect(JSON.parse(form.get("payload") as string)).toEqual({
      name: "demian-lee",
      description: "Servidor de producción",
      executable: "demian-lee",
      args: "",
      restart: "on-failure",
      restart_sec: 5,
      env: [
        { key: "PORT", value: "8080" },
        { key: "SERVER_NAME", value: "Servidor-Produccion-1" },
        { key: "ADMIN_PASSWORD", value: "Secreta123" },
      ],
    });
    expect((form.get("archive") as File).name).toBe("demian-lee.tar.gz");
    expect(screen.queryByText(/revise Estado y Logs/)).not.toBeInTheDocument();
    expect(onDone).toHaveBeenCalled();
    // Los valores secretos salen del formulario al enviarlo (REQ-13).
    for (const input of screen.getAllByLabelText(/^Valor de la variable/)) {
      expect(input).toHaveValue("");
    }
  });

  it("AC-1 the value inputs are masked", async () => {
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await addVariable(user, "PORT", "8080");

    expect(screen.getByLabelText("Valor de la variable 1")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("Clave de la variable 1")).not.toHaveAttribute("type", "password");
  });

  it("AC-14 shows the warning when the service is not active after creation", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(201, { message: "Servicio demian-lee creado en vm1", active_state: "failed" }),
    );
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await fillBasics(user);
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText("Servicio demian-lee creado en vm1")).toBeInTheDocument();
    expect(screen.getByText(/El servicio demian-lee quedó en estado failed; revise Estado y Logs/)).toBeInTheDocument();
  });

  it("AC-3 an invalid name shows the field error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await fillBasics(user, "app_x");
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText(/El nombre del servicio debe tener de 1 a 32 caracteres/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-5 an archive that is not .tar.gz shows the error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await fillBasics(user, "demian-lee", new File(["x"], "app.zip"));
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText("El archivo debe ser .tar.gz")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-9 an invalid variable key shows the row error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await fillBasics(user);
    await addVariable(user, "port", "1");
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText(/La variable 1: la clave debe cumplir/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-9 variable rows can be removed", async () => {
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await addVariable(user, "PORT", "8080");
    await addVariable(user, "OTRA", "x");
    await user.click(screen.getByRole("button", { name: "Quitar variable 1" }));

    expect(screen.getAllByLabelText(/^Clave de la variable/)).toHaveLength(1);
    expect(screen.getByLabelText("Clave de la variable 1")).toHaveValue("OTRA");
  });

  it("AC-37 shows loading and disables the form while the request runs", async () => {
    let resolve: (response: Response) => void = () => undefined;
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise((r) => (resolve = r)));
    const onBusyChange = vi.fn();
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={onBusyChange} onDone={vi.fn()} />);

    await fillBasics(user);
    await addVariable(user, "PORT", "8080");
    await user.click(screen.getByRole("button", { name: /Crear servicio/ }));

    expect(await screen.findByLabelText("Cargando")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Crear servicio/ })).toBeDisabled();
    expect(screen.getByLabelText("Nombre del servicio")).toBeDisabled();
    expect(screen.getByLabelText("Valor de la variable 1")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Agregar variable" })).toBeDisabled();
    expect(onBusyChange).toHaveBeenLastCalledWith(true);

    resolve(jsonResponse(201, { message: "Servicio demian-lee creado en vm1", active_state: "active" }));
    await waitFor(() => expect(screen.getByRole("button", { name: /Crear servicio/ })).toBeEnabled());
    expect(onBusyChange).toHaveBeenLastCalledWith(false);
  });

  it("AC-37 the form is disabled while the screen is busy with another operation", () => {
    render(<CreateServiceForm vm={vm1} busy onBusyChange={vi.fn()} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Crear servicio" })).toBeDisabled();
    expect(screen.getByLabelText("Nombre del servicio")).toBeDisabled();
  });

  it("AC-16 without a VM the button is disabled with a hint", () => {
    render(<CreateServiceForm vm={null} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Crear servicio" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("shows the backend failure message", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(409, { error: "Ya existe un servicio o usuario con ese nombre en la VM" }),
    );
    const user = setup();
    render(<CreateServiceForm vm={vm1} busy={false} onBusyChange={vi.fn()} onDone={vi.fn()} />);

    await fillBasics(user);
    await user.click(screen.getByRole("button", { name: "Crear servicio" }));

    expect(await screen.findByText("Ya existe un servicio o usuario con ese nombre en la VM")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
  });
});
