import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { FrontendForm } from "./FrontendForm";
import type { Lb } from "./types";

const lb1: Lb = { name: "lb1", state: "running", ip: "192.168.1.60", subnet: "10.10.1.0/24", frontend_port: 80, server_count: 0 };

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("FrontendForm", () => {
  it("AC-11 prefills the current port", () => {
    render(<FrontendForm lb={{ ...lb1, frontend_port: 8081 }} onDone={vi.fn()} />);

    expect(screen.getByLabelText("Puerto del frontend")).toHaveValue("8081");
  });

  it("AC-11 invalid port shows the error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<FrontendForm lb={lb1} onDone={vi.fn()} />);

    await user.clear(screen.getByLabelText("Puerto del frontend"));
    await user.type(screen.getByLabelText("Puerto del frontend"), "22");
    await user.click(screen.getByRole("button", { name: "Guardar puerto" }));

    expect(await screen.findByText("El puerto 22 está reservado para SSH")).toBeInTheDocument();
    expect(screen.getByLabelText("Puerto del frontend")).toHaveAttribute("aria-invalid", "true");
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.clear(screen.getByLabelText("Puerto del frontend"));
    await user.type(screen.getByLabelText("Puerto del frontend"), "65536");
    await user.click(screen.getByRole("button", { name: "Guardar puerto" }));

    expect(await screen.findByText("El puerto debe ser un número entre 1 y 65535")).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-11 valid port sends PUT with {port} and shows the result", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse(200, { message: "Frontend de lb1 en el puerto 8080" }));
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<FrontendForm lb={lb1} onDone={onDone} />);

    await user.clear(screen.getByLabelText("Puerto del frontend"));
    await user.type(screen.getByLabelText("Puerto del frontend"), "8080");
    await user.click(screen.getByRole("button", { name: "Guardar puerto" }));

    expect(await screen.findByText("Frontend de lb1 en el puerto 8080")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/haproxy/lbs/lb1/frontend");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(init?.body as string)).toEqual({ port: 8080 });
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-45 shows the reload failure text", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(500, { error: "No se pudo recargar HAProxy; se mantuvo la configuración anterior" }),
    );
    const user = userEvent.setup();
    render(<FrontendForm lb={lb1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Guardar puerto" }));

    expect(await screen.findByText("No se pudo recargar HAProxy; se mantuvo la configuración anterior")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
  });

  it("disables the form without a selected load balancer", () => {
    render(<FrontendForm lb={null} onDone={vi.fn()} />);

    expect(screen.getByLabelText("Puerto del frontend")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Guardar puerto" })).toBeDisabled();
  });

  it("REQ-22 shows a spinner and disables the button while saving", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise<Response>(() => {}));
    const user = userEvent.setup();
    render(<FrontendForm lb={lb1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Guardar puerto" }));

    expect(await screen.findByRole("status", { name: "Cargando" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Guardar puerto/ })).toBeDisabled();
  });
});
