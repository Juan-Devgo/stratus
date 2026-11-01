import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CreateDnsForm } from "./CreateDnsForm";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// Llena el formulario; los campos numéricos se reemplazan por completo.
async function fill(user: ReturnType<typeof userEvent.setup>, values: { name: string; memory: string; cpus: string }) {
  await user.type(screen.getByLabelText("Nombre"), values.name);
  for (const [label, value] of [
    ["Memoria (MB)", values.memory],
    ["CPUs", values.cpus],
  ] as const) {
    await user.clear(screen.getByLabelText(label));
    await user.type(screen.getByLabelText(label), value);
  }
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CreateDnsForm", () => {
  it("AC-22 submits name, memory and CPUs and shows the result", async () => {
    const message = "Servidor DNS dns1 creado en 192.168.1.53; zona stratus.lan";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(201, { message }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<CreateDnsForm busy={false} onDone={onDone} />);

    await fill(user, { name: "dns1", memory: "1024", cpus: "1" });
    await user.click(screen.getByRole("button", { name: "Crear servidor DNS" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/dns/server");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ name: "dns1", memory_mb: 1024, cpus: 1 });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-22 shows the 001 field errors and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateDnsForm busy={false} onDone={vi.fn()} />);

    await fill(user, { name: "DNS_1", memory: "256", cpus: "5" });
    await user.click(screen.getByRole("button", { name: "Crear servidor DNS" }));

    expect(await screen.findByText(/El nombre de la VM debe tener/)).toBeInTheDocument();
    expect(screen.getByText("La memoria debe estar entre 512 y 8192 MB")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-25 shows the backend configuration error", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(400, { error: "Configuración de DNS incompleta: APP_DNS_IP" }));
    const user = userEvent.setup();
    render(<CreateDnsForm busy={false} onDone={vi.fn()} />);

    await fill(user, { name: "dns1", memory: "1024", cpus: "1" });
    await user.click(screen.getByRole("button", { name: "Crear servidor DNS" }));

    expect(await screen.findByText("Configuración de DNS incompleta: APP_DNS_IP")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
  });
});
