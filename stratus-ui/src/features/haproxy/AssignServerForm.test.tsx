import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "@/features/prepare/types";

import { AssignServerForm } from "./AssignServerForm";
import type { Lb } from "./types";

const lb1: Lb = { name: "lb1", state: "running", ip: "192.168.1.60", subnet: "10.10.1.0/24", frontend_port: 80, server_count: 0 };

const vms: Vm[] = [
  { name: "lb1", state: "running", ip: "192.168.1.60", user_key_generated: false, role: "lb", lb: "", lb_ip: "" },
  { name: "web-a", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
  { name: "web-b", state: "poweroff", ip: "", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
  { name: "web-c", state: "running", ip: "10.10.2.11", user_key_generated: false, role: "server", lb: "lb2", lb_ip: "192.168.1.61" },
];

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

async function pickVm(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(screen.getByRole("combobox", { name: "VM a asignar" }));
  await user.click(await screen.findByRole("option", { name }));
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AssignServerForm", () => {
  it("AC-13 lists only plain VMs (not load balancers nor servers)", async () => {
    const user = userEvent.setup();
    render(<AssignServerForm lb={lb1} vms={vms} onDone={vi.fn()} />);

    await user.click(screen.getByRole("combobox", { name: "VM a asignar" }));

    expect(await screen.findByRole("option", { name: "web-a" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "web-b" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "lb1" })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "web-c" })).not.toBeInTheDocument();
  });

  it("AC-13 asks for confirmation warning that the VM restarts, and sends nothing until Confirmar", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(201, { message: "Servidor server1 (web-a) asignado a lb1" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<AssignServerForm lb={lb1} vms={vms} onDone={onDone} />);

    expect(screen.getByLabelText("Puerto del servidor")).toHaveValue("80");
    await pickVm(user, "web-a");
    await user.click(screen.getByRole("button", { name: "Asignar" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("web-a");
    expect(dialog).toHaveTextContent(/se reinicia/i);
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Servidor server1 (web-a) asignado a lb1")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/haproxy/lbs/lb1/servers");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual({ vm: "web-a", port: 80 });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-13 Cancelar closes the dialog and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<AssignServerForm lb={lb1} vms={vms} onDone={vi.fn()} />);

    await pickVm(user, "web-a");
    await user.click(screen.getByRole("button", { name: "Asignar" }));
    await user.click(await screen.findByRole("button", { name: "Cancelar" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-16 invalid server port shows the field error and opens no dialog", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<AssignServerForm lb={lb1} vms={vms} onDone={vi.fn()} />);

    await pickVm(user, "web-a");
    await user.clear(screen.getByLabelText("Puerto del servidor"));
    await user.type(screen.getByLabelText("Puerto del servidor"), "80a");
    await user.click(screen.getByRole("button", { name: "Asignar" }));

    expect(await screen.findByText("El puerto debe ser un número entre 1 y 65535")).toBeInTheDocument();
    expect(screen.getByLabelText("Puerto del servidor")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-15 shows the rejection text from the backend", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(409, { error: "La VM no se puede asignar a este balanceador" }),
    );
    const user = userEvent.setup();
    render(<AssignServerForm lb={lb1} vms={vms} onDone={vi.fn()} />);

    await pickVm(user, "web-b");
    await user.click(screen.getByRole("button", { name: "Asignar" }));
    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("La VM no se puede asignar a este balanceador")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("disables Asignar without a load balancer or without a chosen VM", async () => {
    const { rerender } = render(<AssignServerForm lb={null} vms={vms} onDone={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Asignar" })).toBeDisabled();

    rerender(<AssignServerForm lb={lb1} vms={vms} onDone={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Asignar" })).toBeDisabled();
  });
});
