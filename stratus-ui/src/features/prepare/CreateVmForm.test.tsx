import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CreateVmForm } from "./CreateVmForm";

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

describe("CreateVmForm", () => {
  it("AC-1 submits the parsed input", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    render(<CreateVmForm pending={false} result={null} onSubmit={onSubmit} />);

    await fill(user, { name: "vm1", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(screen.queryByLabelText(/Disco/)).not.toBeInTheDocument();
    expect(onSubmit).toHaveBeenCalledWith({ name: "vm1", memory_mb: 2048, cpus: 2 });
  });

  it("AC-2 shows validation error and does not submit", async () => {
    const onSubmit = vi.fn();
    const user = userEvent.setup();
    render(<CreateVmForm pending={false} result={null} onSubmit={onSubmit} />);

    await fill(user, { name: "-vm", memory: "2048", cpus: "2" });
    await user.click(screen.getByRole("button", { name: "Crear VM" }));

    expect(await screen.findByText(/El nombre de la VM debe tener/)).toBeInTheDocument();
    expect(screen.getByLabelText("Nombre")).toHaveAttribute("aria-invalid", "true");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("AC-21 shows spinner and disables the button while pending", () => {
    render(<CreateVmForm pending result={null} onSubmit={vi.fn()} />);

    expect(screen.getByRole("status", { name: "Cargando" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Crear VM/ })).toBeDisabled();
  });

  it("AC-1 shows the success message", () => {
    const text = "VM vm1 creada y encendida; IP 192.168.1.50";
    render(<CreateVmForm pending={false} result={{ ok: true, text }} onSubmit={vi.fn()} />);

    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it("AC-41 shows the failure message as an error", () => {
    const text = "La operación create_vm falló (código 6): Disco base no encontrado: /vms/base.vdi";
    render(<CreateVmForm pending={false} result={{ ok: false, text }} onSubmit={vi.fn()} />);

    expect(screen.getByText(/Disco base no encontrado/)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });
});
