import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "@/features/prepare/types";

import { CreateUserForm } from "./CreateUserForm";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type User = ReturnType<typeof userEvent.setup>;

async function fillPasswords(user: User, password: string, confirmation = password) {
  await user.type(screen.getByLabelText("Contraseña"), password);
  await user.type(screen.getByLabelText("Confirmar contraseña"), confirmation);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CreateUserForm", () => {
  it("AC-4 submits with sudo and keys and shows download and usage", async () => {
    const message = "Usuario ana creado en vm1. Descargue su llave privada ahora";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(201, { message, key_available: true }),
    );
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={onDone} />);

    await user.type(screen.getByLabelText("Nombre de usuario"), "ana");
    await user.click(screen.getByRole("checkbox", { name: "Privilegios de sudo" }));
    await user.click(screen.getByRole("checkbox", { name: "Generar llaves SSH" }));
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/users/vms/vm1/users");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ username: "ana", sudo: true, ssh_key: true });
    expect(screen.getByRole("button", { name: "Descargar llave privada" })).toBeInTheDocument();
    const usage = screen.getByText("Uso correcto de la llave privada").closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("ssh -i id_ed25519_vm1_ana ana@192.168.1.50");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-4 with a password shows the result and no download", async () => {
    const message = "Usuario luis creado en vm1; inicia sesión con su contraseña en la consola o con su - luis";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(201, { message, key_available: false }),
    );
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Nombre de usuario"), "luis");
    await fillPasswords(user, "Secreta123");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(JSON.parse(fetchSpy.mock.calls[0][1]?.body as string)).toEqual({
      username: "luis",
      sudo: false,
      ssh_key: false,
      password: "Secreta123",
    });
    expect(screen.queryByRole("button", { name: "Descargar llave privada" })).not.toBeInTheDocument();
    // La contraseña no queda en el formulario después de enviarla (REQ-9).
    expect(screen.getByLabelText("Contraseña")).toHaveValue("");
    expect(screen.getByLabelText("Confirmar contraseña")).toHaveValue("");
  });

  it("AC-5 invalid username shows error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Nombre de usuario"), "ana;rm");
    await fillPasswords(user, "Secreta123");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText(/El nombre de usuario debe tener/)).toBeInTheDocument();
    expect(screen.getByLabelText("Nombre de usuario")).toHaveAttribute("aria-invalid", "true");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-7 invalid password shows error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Nombre de usuario"), "ana");
    await fillPasswords(user, "Corta12");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText(/La contraseña debe tener de 8 a 72/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-7 a confirmation that differs shows error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Nombre de usuario"), "ana");
    await fillPasswords(user, "Secreta123", "Secreta124");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText("La confirmación no coincide con la contraseña")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-8 with keys shows no password fields and sends none", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(201, { message: "Usuario ana creado en vm1", key_available: true }),
    );
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={vi.fn()} />);
    expect(screen.getByLabelText("Contraseña")).toBeInTheDocument();

    await user.type(screen.getByLabelText("Nombre de usuario"), "ana");
    await fillPasswords(user, "Secreta123");
    await user.click(screen.getByRole("checkbox", { name: "Generar llaves SSH" }));

    expect(screen.queryByLabelText("Contraseña")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Confirmar contraseña")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Crear usuario" }));
    await screen.findByText("Usuario ana creado en vm1");
    const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
    expect(body).toEqual({ username: "ana", sudo: false, ssh_key: true });
    expect("password" in body).toBe(false);
  });

  it("AC-2 with no VM the button is disabled with a hint", () => {
    render(<CreateUserForm vm={null} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Crear usuario" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-6 shows the backend failure message", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: "El usuario ya existe en la VM" }));
    const user = userEvent.setup();
    render(<CreateUserForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Nombre de usuario"), "ana");
    await fillPasswords(user, "Secreta123");
    await user.click(screen.getByRole("button", { name: "Crear usuario" }));

    expect(await screen.findByText("El usuario ya existe en la VM")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
  });
});
