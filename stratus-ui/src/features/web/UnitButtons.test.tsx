import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { webAction, webStatus } from "./api";
import { UnitButtons } from "./UnitButtons";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function renderButtons(onDone = vi.fn()) {
  return render(
    <UnitButtons
      name="web-a"
      unit="nginx"
      busy={false}
      onAction={(action) => webAction("web-a", action)}
      onStatus={() => webStatus("web-a")}
      onDone={onDone}
    />,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("UnitButtons", () => {
  it("AC-18 Iniciar, Detener and Reiniciar call their actions", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(() => Promise.resolve(jsonResponse(200, { message: "Hecho", active_state: "active" })));
    const onDone = vi.fn();
    const user = userEvent.setup();
    renderButtons(onDone);

    for (const label of ["Iniciar", "Detener", "Reiniciar"]) {
      await user.click(screen.getByRole("button", { name: label }));
      await screen.findByText("Hecho");
    }

    const calls = fetchSpy.mock.calls.map(([url, init]) => [url, init?.method]);
    expect(calls).toEqual([
      ["/api/web/servers/web-a/start", "POST"],
      ["/api/web/servers/web-a/stop", "POST"],
      ["/api/web/servers/web-a/restart", "POST"],
    ]);
    expect(onDone).toHaveBeenCalledTimes(3);
  });

  it("AC-20 Estado shows the output with failed and no error Alert", async () => {
    const output = "× nginx.service - A high performance web server\n     Active: failed (Result: exit-code)";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { output }));
    const user = userEvent.setup();
    renderButtons();

    await user.click(screen.getByRole("button", { name: "Estado" }));

    expect(await screen.findByText(/Active: failed \(Result: exit-code\)/)).toBeInTheDocument();
    expect(screen.getByText("Salida")).toBeInTheDocument();
    expect(screen.queryByText("La operación falló")).not.toBeInTheDocument();
  });

  it("AC-21 start that ends failed warns to check Estado", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(200, { message: "nginx iniciado en web-a", active_state: "failed" }),
    );
    const user = userEvent.setup();
    renderButtons();

    await user.click(screen.getByRole("button", { name: "Iniciar" }));

    expect(await screen.findByText("nginx iniciado en web-a")).toBeInTheDocument();
    expect(screen.getByText("El servicio nginx quedó en estado failed; revise Estado")).toBeInTheDocument();
  });
});
