import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CreateWebForm } from "./CreateWebForm";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type User = ReturnType<typeof userEvent.setup>;

// Llena el formulario; los campos numéricos se reemplazan por completo. Sin type no elige tipo.
async function fill(user: User, values: { name: string; memory: string; cpus: string; type?: string }) {
  await user.type(screen.getByLabelText("Nombre"), values.name);
  for (const [label, value] of [
    ["Memoria (MB)", values.memory],
    ["CPUs", values.cpus],
  ] as const) {
    await user.clear(screen.getByLabelText(label));
    await user.type(screen.getByLabelText(label), value);
  }
  if (values.type) {
    await user.click(screen.getByRole("combobox", { name: "Tipo" }));
    await user.click(await screen.findByRole("option", { name: values.type }));
  }
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CreateWebForm", () => {
  it("AC-1 submits name, memory, CPUs and type and shows the result", async () => {
    const message = "Servidor web web-a (Nginx) creado; IP 192.168.1.40";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(201, { message }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<CreateWebForm busy={false} onDone={onDone} />);

    await fill(user, { name: "web-a", memory: "1024", cpus: "1", type: "Nginx" });
    await user.click(screen.getByRole("button", { name: "Crear servidor web" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/web/servers");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ name: "web-a", memory_mb: 1024, cpus: 1, type: "nginx" });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-2 shows the 001 field errors and the type error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<CreateWebForm busy={false} onDone={vi.fn()} />);

    await fill(user, { name: "Web_1", memory: "256", cpus: "5" });
    await user.click(screen.getByRole("button", { name: "Crear servidor web" }));

    expect(await screen.findByText(/El nombre de la VM debe tener/)).toBeInTheDocument();
    expect(screen.getByText("La memoria debe estar entre 512 y 8192 MB")).toBeInTheDocument();
    expect(screen.getByText("El número de CPUs debe estar entre 1 y 4")).toBeInTheDocument();
    expect(screen.getByText("Seleccione el tipo de servidor web")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-6 shows the success and the DNS warning", async () => {
    const warning = "Servidor web creado; no se pudo registrar en DNS, agréguelo desde DNS";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(201, { message: "Servidor web web-a (Apache) creado; IP 192.168.1.40", warning }),
    );
    const user = userEvent.setup();
    render(<CreateWebForm busy={false} onDone={vi.fn()} />);

    await fill(user, { name: "web-a", memory: "1024", cpus: "1", type: "Apache" });
    await user.click(screen.getByRole("button", { name: "Crear servidor web" }));

    expect(await screen.findByText("Operación completada")).toBeInTheDocument();
    expect(screen.getByText("Advertencia")).toBeInTheDocument();
    expect(screen.getByText(warning)).toBeInTheDocument();
  });

  it("REQ-38 busy disables the button", () => {
    render(<CreateWebForm busy={true} onDone={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Crear servidor web" })).toBeDisabled();
  });
});
