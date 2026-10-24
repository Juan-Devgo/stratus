import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { IssueForm } from "./IssueForm";
import type { Vm } from "./types";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("IssueForm", () => {
  it("AC-14 live preview updates", async () => {
    const user = userEvent.setup();
    render(<IssueForm vm={vm1} onDone={vi.fn()} />);
    const preview = screen.getByLabelText("Vista previa de /etc/issue");

    await user.type(screen.getByLabelText("Mensaje"), "Hola");
    expect(preview).toHaveTextContent(/^Hola$/);

    // Se marcan en desorden; la vista previa sigue el orden fijo de REQ-26.
    await user.click(screen.getByRole("checkbox", { name: "Usuarios" }));
    await user.click(screen.getByRole("checkbox", { name: "Host" }));

    expect(preview.textContent).toBe("Hola\nHost: \\n\nUsuarios: \\U");
  });

  it("AC-14 applies the issue and shows result", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "/etc/issue de vm1 actualizado" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<IssueForm vm={vm1} onDone={onDone} />);

    await user.type(screen.getByLabelText("Mensaje"), "Hola");
    await user.click(screen.getByRole("checkbox", { name: "Usuarios" }));
    await user.click(screen.getByRole("checkbox", { name: "Host" }));
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText("/etc/issue de vm1 actualizado")).toBeInTheDocument();
    expect(screen.getByText("Operación completada")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1/issue");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ message: "Hola", fields: ["hostname", "users"] });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-15 long message shows error", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<IssueForm vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByLabelText("Mensaje"));
    await user.paste("a".repeat(501));
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText(/como máximo 500 caracteres/)).toBeInTheDocument();
    expect(screen.getByLabelText("Mensaje")).toHaveAttribute("aria-invalid", "true");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-16 empty issue shows error", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<IssueForm vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText("Seleccione al menos un campo o escriba un mensaje")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-25 backslash message rejected", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<IssueForm vm={vm1} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Mensaje"), "a\\b");
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText(/sin barra invertida/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-5 issue form disabled without VM", () => {
    render(<IssueForm vm={null} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Aplicar" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-43 shows no-IP message", async () => {
    const noIp = "La VM no tiene dirección IP; verifique que esté encendida";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: noIp }));
    const user = userEvent.setup();
    render(<IssueForm vm={{ ...vm1, state: "poweroff", ip: "" }} onDone={vi.fn()} />);

    await user.type(screen.getByLabelText("Mensaje"), "Hola");
    await user.click(screen.getByRole("button", { name: "Aplicar" }));

    expect(await screen.findByText(noIp)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });
});
