import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { PackageForm } from "./PackageForm";
import type { Vm } from "./types";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };
const catalog = ["openssh-server", "haproxy", "nginx", "apache2", "curl", "htop", "git", "ufw"];
const SSH_BLOCKED = "No se puede eliminar openssh-server: el backend lo usa para conectarse";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("PackageForm", () => {
  it("AC-6 install from catalog sends package", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse(200, { message: "Paquete nginx instalado en vm1" }))
      .mockResolvedValueOnce(jsonResponse(200, { message: "Paquete nginx eliminado de vm1" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<PackageForm vm={vm1} catalog={catalog} onDone={onDone} />);

    await user.click(screen.getByRole("combobox", { name: "Catálogo" }));
    await user.click(await screen.findByRole("option", { name: "nginx" }));
    expect(screen.getByLabelText("Paquete")).toHaveValue("nginx");

    await user.click(screen.getByRole("button", { name: "Instalar" }));

    expect(await screen.findByText("Paquete nginx instalado en vm1")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1/install-package");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ package: "nginx" });
    expect(onDone).toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Eliminar" }));

    expect(await screen.findByText("Paquete nginx eliminado de vm1")).toBeInTheDocument();
    const [removeUrl, removeInit] = fetchSpy.mock.calls[1];
    expect(removeUrl).toBe("/api/prepare/vms/vm1/remove-package");
    expect(JSON.parse(removeInit?.body as string)).toEqual({ package: "nginx" });
  });

  it("AC-7 rejects injected package name", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<PackageForm vm={vm1} catalog={catalog} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Paquete"), "nginx; rm -rf /");
    await user.click(screen.getByRole("button", { name: "Instalar" }));

    expect(await screen.findByText(/El nombre del paquete debe tener/)).toBeInTheDocument();
    expect(screen.getByLabelText("Paquete")).toHaveAttribute("aria-invalid", "true");

    await user.clear(screen.getByLabelText("Paquete"));
    await user.click(screen.getByRole("button", { name: "Eliminar" }));

    expect(await screen.findByText(/El nombre del paquete debe tener/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-8 shows failure message", async () => {
    const error = "La operación install_package falló (código 5): E: Unable to locate package foo";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(500, { error }));
    const user = userEvent.setup();
    render(<PackageForm vm={vm1} catalog={catalog} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Paquete"), "foo");
    await user.click(screen.getByRole("button", { name: "Instalar" }));

    expect(await screen.findByText(error)).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
  });

  it("AC-33 shows openssh-server removal block", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: SSH_BLOCKED }));
    const user = userEvent.setup();
    render(<PackageForm vm={vm1} catalog={catalog} onDone={vi.fn()} />);

    await user.click(screen.getByRole("combobox", { name: "Catálogo" }));
    await user.click(await screen.findByRole("option", { name: "openssh-server" }));
    await user.click(screen.getByRole("button", { name: "Eliminar" }));

    expect(await screen.findByText(SSH_BLOCKED)).toBeInTheDocument();
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/prepare/vms/vm1/remove-package");
  });

  it("AC-5 package form disabled without VM", () => {
    render(<PackageForm vm={null} catalog={catalog} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Instalar" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Eliminar" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-43 shows no-IP message", async () => {
    const noIp = "La VM no tiene dirección IP; verifique que esté encendida";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: noIp }));
    const user = userEvent.setup();
    render(<PackageForm vm={{ ...vm1, state: "poweroff", ip: "" }} catalog={catalog} onDone={vi.fn()} />);

    await user.click(screen.getByRole("combobox", { name: "Catálogo" }));
    await user.click(await screen.findByRole("option", { name: "nginx" }));
    await user.click(screen.getByRole("button", { name: "Instalar" }));

    expect(await screen.findByText(noIp)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });
});
