import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { HostnameForm } from "./HostnameForm";
import type { Vm } from "./types";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };
const TIMEOUT = "La operación superó el tiempo límite de 15 minutos";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("HostnameForm", () => {
  it("AC-17 submits hostname", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Hostname de vm1 cambiado a web-01" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<HostnameForm vm={vm1} onDone={onDone} />);

    await user.type(screen.getByLabelText("Hostname"), "web-01");
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText("Hostname de vm1 cambiado a web-01")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1/hostname");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ hostname: "web-01" });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-18 invalid hostname shows error", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<HostnameForm vm={vm1} onDone={vi.fn()} />);

    for (const hostname of ["Web_01", "-web", "a".repeat(64)]) {
      await user.clear(screen.getByLabelText("Hostname"));
      await user.type(screen.getByLabelText("Hostname"), hostname);
      await user.click(screen.getByRole("button", { name: "Aplicar" }));

      expect(await screen.findByText(/El hostname debe tener/)).toBeInTheDocument();
      expect(screen.getByLabelText("Hostname")).toHaveAttribute("aria-invalid", "true");
    }
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-20 shows timeout failure", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(504, { error: TIMEOUT }));
    const user = userEvent.setup();
    render(<HostnameForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Hostname"), "web-01");
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText(TIMEOUT)).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("AC-43 shows no-IP message", async () => {
    const noIp = "La VM no tiene dirección IP; verifique que esté encendida";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: noIp }));
    const user = userEvent.setup();
    render(<HostnameForm vm={{ ...vm1, state: "poweroff", ip: "" }} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Hostname"), "web-01");
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText(noIp)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("AC-5 hostname form disabled without VM", () => {
    render(<HostnameForm vm={null} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Aplicar" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });
});
