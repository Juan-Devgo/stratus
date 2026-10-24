import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CreateVmForm } from "./CreateVmForm";

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

describe("CreateVmForm", () => {
  it("AC-1 submits create form and shows result", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(201, { message: "VM vm1 creada y encendida; IP 192.168.1.50" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<CreateVmForm onDone={onDone} />);

    await fill(user, { name: "vm1", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(await screen.findByText("VM vm1 creada y encendida; IP 192.168.1.50")).toBeInTheDocument();
    expect(screen.queryByLabelText(/Disco/)).not.toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ name: "vm1", memory_mb: 2048, cpus: 2 });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-2 shows validation error and does not call API", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateVmForm onDone={vi.fn()} />);

    await fill(user, { name: "-vm", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(await screen.findByText(/El nombre de la VM debe tener/)).toBeInTheDocument();
    expect(screen.getByLabelText("Nombre")).toHaveAttribute("aria-invalid", "true");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-29 shows duplicate message", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: "Ya existe una VM con ese nombre" }));
    const user = userEvent.setup();
    render(<CreateVmForm onDone={vi.fn()} />);

    await fill(user, { name: "vm1", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(await screen.findByText("Ya existe una VM con ese nombre")).toBeInTheDocument();
  });

  it("AC-46 shows create-in-progress message", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: "Ya hay una creación de VM en curso" }));
    const user = userEvent.setup();
    render(<CreateVmForm onDone={vi.fn()} />);

    await fill(user, { name: "vm2", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(await screen.findByText("Ya hay una creación de VM en curso")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("AC-41 shows base disk error", async () => {
    const message = "La operación create_vm falló (código 6): Disco base no encontrado: /vms/base.vdi";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(500, { error: message }));
    const user = userEvent.setup();
    render(<CreateVmForm onDone={vi.fn()} />);

    await fill(user, { name: "vm1", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(await screen.findByText(/Disco base no encontrado/)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("AC-21 shows spinner and disables buttons while pending", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));
    const user = userEvent.setup();
    render(<CreateVmForm onDone={vi.fn()} />);

    await fill(user, { name: "vm1", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: /Crear VM/ }));

    expect(await screen.findByRole("status", { name: "Cargando" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Crear VM/ })).toBeDisabled();
  });
});
