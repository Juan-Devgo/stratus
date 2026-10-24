import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CreateLbForm } from "./CreateLbForm";

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

describe("CreateLbForm", () => {
  it("AC-1 submits name, memory and CPUs and shows the result", async () => {
    const message = "Balanceador lb1 creado y encendido; IP 192.168.1.60; subred 10.10.1.0/24";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(201, { message }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<CreateLbForm busy={false} onDone={onDone} />);

    await fill(user, { name: "lb1", memory: "1024", cpus: "1" });
    await user.click(screen.getByRole("button", { name: "Crear balanceador" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/haproxy/lbs");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ name: "lb1", memory_mb: 1024, cpus: 1 });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-2 shows the 001 field errors and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateLbForm busy={false} onDone={vi.fn()} />);

    await fill(user, { name: "LB_1", memory: "256", cpus: "5" });
    await user.click(screen.getByRole("button", { name: "Crear balanceador" }));

    expect(await screen.findByText(/El nombre de la VM debe tener/)).toBeInTheDocument();
    expect(screen.getByText("La memoria debe estar entre 512 y 8192 MB")).toBeInTheDocument();
    expect(screen.getByText("El número de CPUs debe estar entre 1 y 4")).toBeInTheDocument();
    expect(screen.getByLabelText("Nombre")).toHaveAttribute("aria-invalid", "true");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-3 shows the backend failure text", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(500, { error: "La operación create_lb falló (código 6): Disco base de HAProxy no encontrado: /vms/h.vdi" }),
    );
    const user = userEvent.setup();
    render(<CreateLbForm busy={false} onDone={vi.fn()} />);

    await fill(user, { name: "lb1", memory: "1024", cpus: "1" });
    await user.click(screen.getByRole("button", { name: "Crear balanceador" }));

    expect(await screen.findByText(/Disco base de HAProxy no encontrado/)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("REQ-22 shows a spinner while the request runs and disables the button; busy disables it too", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));
    const user = userEvent.setup();
    const { rerender } = render(<CreateLbForm busy={true} onDone={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Crear balanceador" })).toBeDisabled();

    rerender(<CreateLbForm busy={false} onDone={vi.fn()} />);
    await fill(user, { name: "lb1", memory: "1024", cpus: "1" });
    await user.click(screen.getByRole("button", { name: /Crear balanceador/ }));

    expect(await screen.findByRole("status", { name: "Cargando" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Crear balanceador/ })).toBeDisabled();
  });
});
