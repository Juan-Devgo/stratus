import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { InstallPageDialog } from "./InstallPageDialog";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// El filtro "accept" del campo no debe esconder el .zip de la prueba de validación.
function setup() {
  return userEvent.setup({ applyAccept: false, delay: null });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("InstallPageDialog", () => {
  it("AC-11 warns that /var/www/html is replaced and posts the archive only after Confirmar", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: "Página instalada en web-a" }));
    const onDone = vi.fn();
    const user = setup();
    render(<InstallPageDialog vm="web-a" onClose={vi.fn()} onDone={onDone} />);

    const file = new File(["<html></html>"], "site.tar.gz");
    await user.upload(await screen.findByLabelText("Archivo de la página (.tar.gz)"), file);
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/Se reemplazará todo \/var\/www\/html de web-a/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Página instalada en web-a")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/web/servers/web-a/page");
    expect(init?.method).toBe("POST");
    const body = init?.body as FormData;
    expect([...body.keys()]).toEqual(["archive"]);
    expect((body.get("archive") as File).name).toBe("site.tar.gz");
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-13 shows the file error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    render(<InstallPageDialog vm="web-a" onClose={vi.fn()} onDone={vi.fn()} />);

    await user.upload(await screen.findByLabelText("Archivo de la página (.tar.gz)"), new File(["x"], "site.zip"));
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    expect(await screen.findByText("El archivo debe ser .tar.gz")).toBeInTheDocument();
    expect(screen.queryByText(/Se reemplazará todo/)).not.toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
