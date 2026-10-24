import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { RedeployServiceDialog } from "./RedeployServiceDialog";
import type { ServiceRow } from "./types";

const ROW: ServiceRow = {
  name: "demian-lee",
  description: "Servidor de producción",
  active_state: "active",
  enabled_state: "enabled",
  restart: "on-failure",
  restart_sec: 5,
  executable: "demian-lee",
  args: "",
  env_keys: ["PORT"],
};

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type User = ReturnType<typeof userEvent.setup>;

// El filtro "accept" del campo no debe esconder el .zip de la prueba de validación.
function setup() {
  return userEvent.setup({ applyAccept: false, delay: null });
}

async function fill(user: User, element: HTMLElement, text: string) {
  await user.clear(element);
  await user.click(element);
  await user.paste(text);
}

function renderDialog(overrides: { onDone?: () => void; onClose?: () => void } = {}) {
  return render(
    <RedeployServiceDialog
      vm="vm1"
      service={ROW}
      onClose={overrides.onClose ?? vi.fn()}
      onDone={overrides.onDone ?? vi.fn()}
    />,
  );
}

async function chooseArchive(user: User, name = "nuevo.tar.gz") {
  await user.upload(await screen.findByLabelText("Archivo de la aplicación (.tar.gz)"), new File(["datos"], name));
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("RedeployServiceDialog", () => {
  it("AC-38 prefills the executable, warns before replacing and sends payload before archive", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(200, { message: "Servicio demian-lee redesplegado en vm1", active_state: "active" }),
    );
    const onDone = vi.fn();
    const user = setup();
    renderDialog({ onDone });

    const executable = await screen.findByLabelText("Ejecutable");
    expect(executable).toHaveValue("demian-lee");
    await fill(user, executable, "bin/server");
    await chooseArchive(user);
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    // Segundo paso: advertencia, y todavía no se envía nada.
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/Se reemplaza todo el contenido de \/opt\/demian-lee, incluidos los archivos de datos/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Servicio demian-lee redesplegado en vm1")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/services/vms/vm1/services/demian-lee/redeploy");
    expect(init?.method).toBe("POST");
    const form = init?.body as FormData;
    expect([...form.keys()]).toEqual(["payload", "archive"]);
    expect(JSON.parse(form.get("payload") as string)).toEqual({ executable: "bin/server" });
    expect((form.get("archive") as File).name).toBe("nuevo.tar.gz");
    expect(screen.queryByText(/revise Estado y Logs/)).toBeNull();
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-27 shows the warning when the new app is not active after the redeploy", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(200, { message: "Servicio demian-lee redesplegado en vm1", active_state: "failed" }),
    );
    const user = setup();
    renderDialog();

    await chooseArchive(user);
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(/El servicio demian-lee quedó en estado failed; revise Estado y Logs/)).toBeInTheDocument();
  });

  it("AC-38 shows the backend rejection when the executable is not in the new archive", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(422, { error: "El ejecutable no existe en el directorio de la aplicación" }),
    );
    const user = setup();
    renderDialog();

    await fill(user, await screen.findByLabelText("Ejecutable"), "bin/otro");
    await chooseArchive(user);
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("El ejecutable no existe en el directorio de la aplicación")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cerrar" })).toBeInTheDocument();
  });

  it("AC-5 an archive that is not .tar.gz shows the error and does not reach the confirmation", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    renderDialog();

    await chooseArchive(user, "app.zip");
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    expect(await screen.findByText("El archivo debe ser .tar.gz")).toBeInTheDocument();
    expect(screen.queryByText(/Se reemplaza todo el contenido/)).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-7 an invalid executable shows the error and does not reach the confirmation", async () => {
    const user = setup();
    renderDialog();

    await fill(user, await screen.findByLabelText("Ejecutable"), "../x");
    await chooseArchive(user);
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    expect(await screen.findByText(/El ejecutable debe ser una ruta relativa/)).toBeInTheDocument();
    expect(screen.queryByText(/Se reemplaza todo el contenido/)).toBeNull();
  });

  it("a missing archive asks for one", async () => {
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("button", { name: "Continuar" }));

    expect(await screen.findByText("Seleccione el archivo .tar.gz de la aplicación")).toBeInTheDocument();
  });

  it("Atrás goes back to the form keeping the chosen values", async () => {
    const user = setup();
    renderDialog();

    await fill(user, await screen.findByLabelText("Ejecutable"), "bin/server");
    await chooseArchive(user);
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Atrás" }));

    expect(screen.getByLabelText("Ejecutable")).toHaveValue("bin/server");
    expect(screen.queryByText(/Se reemplaza todo el contenido/)).toBeNull();
  });

  it("AC-37 shows loading and disables the buttons while redeploying", async () => {
    let resolve: (response: Response) => void = () => undefined;
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise((r) => (resolve = r)));
    const user = setup();
    renderDialog();

    await chooseArchive(user);
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByLabelText("Cargando")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Confirmar/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Atrás" })).toBeDisabled();

    resolve(jsonResponse(200, { message: "Servicio demian-lee redesplegado en vm1", active_state: "active" }));
    expect(await screen.findByRole("button", { name: "Cerrar" })).toBeEnabled();
  });

  it("renders nothing when there is no service", () => {
    render(<RedeployServiceDialog vm="vm1" service={null} onClose={vi.fn()} onDone={vi.fn()} />);

    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
