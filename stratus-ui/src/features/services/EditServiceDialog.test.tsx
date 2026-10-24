import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { EditServiceDialog } from "./EditServiceDialog";
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
  env_keys: ["PORT", "SERVER_NAME", "ADMIN_PASSWORD"],
};

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type User = ReturnType<typeof userEvent.setup>;

function setup() {
  return userEvent.setup({ delay: null });
}

// Pegar es mucho más rápido que escribir carácter por carácter y deja el mismo valor en el campo.
async function fill(user: User, element: HTMLElement, text: string) {
  await user.click(element);
  await user.paste(text);
}

function renderDialog(overrides: { onDone?: () => void; onClose?: () => void } = {}) {
  return render(
    <EditServiceDialog
      vm="vm1"
      service={ROW}
      onClose={overrides.onClose ?? vi.fn()}
      onDone={overrides.onDone ?? vi.fn()}
    />,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("EditServiceDialog", () => {
  it("AC-23 prefills the current values and the keys without values", async () => {
    renderDialog();

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Descripción")).toHaveValue("Servidor de producción");
    expect(within(dialog).getByLabelText("Ejecutable")).toHaveValue("demian-lee");
    expect(within(dialog).getByLabelText("Argumentos")).toHaveValue("");
    expect(within(dialog).getByLabelText("RestartSec")).toHaveValue("5");
    expect(within(dialog).getByRole("combobox", { name: "Restart" })).toHaveTextContent("on-failure");
    expect(within(dialog).getByRole("checkbox", { name: "Reemplazar variables de entorno" })).not.toBeChecked();
    for (const key of ["PORT", "SERVER_NAME", "ADMIN_PASSWORD"]) {
      expect(within(dialog).getByText(key)).toBeInTheDocument();
    }
    // El nombre no se puede cambiar: se muestra en el título, no en un campo.
    expect(within(dialog).getByText(/demian-lee/, { selector: "h2, [data-slot='dialog-title']" })).toBeInTheDocument();
    expect(within(dialog).queryByLabelText("Nombre del servicio")).toBeNull();
    // Sin reemplazo no hay campos de valores.
    expect(within(dialog).queryByLabelText(/^Valor de la variable/)).toBeNull();
  });

  it("AC-23 saves Restart always without touching the variables", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(200, { message: "Servicio demian-lee actualizado en vm1", active_state: "active" }),
    );
    const onDone = vi.fn();
    const user = setup();
    renderDialog({ onDone });

    await user.click(await screen.findByRole("combobox", { name: "Restart" }));
    await user.click(await screen.findByRole("option", { name: "always" }));
    await user.click(screen.getByRole("button", { name: "Guardar" }));

    expect(await screen.findByText("Servicio demian-lee actualizado en vm1")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/services/vms/vm1/services/demian-lee");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(init?.body as string)).toEqual({
      description: "Servidor de producción",
      executable: "demian-lee",
      args: "",
      restart: "always",
      restart_sec: 5,
      replace_env: false,
      env: [],
    });
    expect(screen.queryByText(/revise Estado y Logs/)).toBeNull();
    expect(onDone).toHaveBeenCalled();
  });

  it("AC-40 shows the warning when the app is not active after the restart", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(200, { message: "Servicio demian-lee actualizado en vm1", active_state: "failed" }),
    );
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("button", { name: "Guardar" }));

    expect(await screen.findByText(/El servicio demian-lee quedó en estado failed; revise Estado y Logs/)).toBeInTheDocument();
  });

  it("AC-25 a checked box asks every value again, keys prefilled and values empty and masked", async () => {
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("checkbox", { name: "Reemplazar variables de entorno" }));

    const keys = screen.getAllByLabelText(/^Clave de la variable/);
    expect(keys.map((input) => (input as HTMLInputElement).value)).toEqual(["PORT", "SERVER_NAME", "ADMIN_PASSWORD"]);
    // Las claves prefijadas no se editan.
    for (const key of keys) {
      expect(key).toHaveAttribute("readonly");
    }
    const values = screen.getAllByLabelText(/^Valor de la variable/);
    expect(values).toHaveLength(3);
    for (const value of values) {
      expect(value).toHaveValue("");
      expect(value).toHaveAttribute("type", "password");
    }
  });

  it("AC-25 saves only the variables that remain", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(200, { message: "Servicio demian-lee actualizado en vm1", active_state: "active" }),
    );
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("checkbox", { name: "Reemplazar variables de entorno" }));
    // Quitar ADMIN_PASSWORD y SERVER_NAME, dejar PORT con 9090.
    await user.click(screen.getByRole("button", { name: "Quitar variable 3" }));
    await user.click(screen.getByRole("button", { name: "Quitar variable 2" }));
    await fill(user, screen.getByLabelText("Valor de la variable 1"), "9090");
    await user.click(screen.getByRole("button", { name: "Guardar" }));

    expect(await screen.findByText("Servicio demian-lee actualizado en vm1")).toBeInTheDocument();
    const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
    expect(body.replace_env).toBe(true);
    expect(body.env).toEqual([{ key: "PORT", value: "9090" }]);
  });

  it("AC-25 an invalid variable shows the row error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("checkbox", { name: "Reemplazar variables de entorno" }));
    await fill(user, screen.getByLabelText("Valor de la variable 1"), "café");
    await user.click(screen.getByRole("button", { name: "Guardar" }));

    expect(await screen.findByText(/La variable 1: el valor admite hasta 1024 caracteres ASCII imprimibles/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-8 an invalid field shows the error and sends nothing", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = setup();
    renderDialog();

    await fill(user, await screen.findByLabelText("Argumentos"), "a;b");
    await user.click(screen.getByRole("button", { name: "Guardar" }));

    expect(await screen.findByText(/Los argumentos deben ser hasta 16 tokens/)).toBeInTheDocument();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-11 the value inputs are masked and the dialog never shows a value", async () => {
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("checkbox", { name: "Reemplazar variables de entorno" }));
    await fill(user, screen.getByLabelText("Valor de la variable 1"), "Secreta123");

    expect(screen.getByLabelText("Valor de la variable 1")).toHaveAttribute("type", "password");
    expect(screen.queryByText("Secreta123")).toBeNull();
  });

  it("AC-37 shows loading and disables the fields and buttons while saving", async () => {
    let resolve: (response: Response) => void = () => undefined;
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise((r) => (resolve = r)));
    const user = setup();
    renderDialog();

    await user.click(await screen.findByRole("button", { name: "Guardar" }));

    expect(await screen.findByLabelText("Cargando")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Guardar/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancelar" })).toBeDisabled();
    expect(screen.getByLabelText("Descripción")).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "Reemplazar variables de entorno" })).toHaveAttribute("aria-disabled", "true");

    resolve(jsonResponse(200, { message: "Servicio demian-lee actualizado en vm1", active_state: "active" }));
    expect(await screen.findByRole("button", { name: "Cerrar" })).toBeEnabled();
  });

  it("shows the backend failure inside the dialog and closes with Cerrar", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(422, { error: "El ejecutable no existe en el directorio de la aplicación" }),
    );
    const onClose = vi.fn();
    const user = setup();
    renderDialog({ onClose });

    await user.click(await screen.findByRole("button", { name: "Guardar" }));

    expect(await screen.findByText("El ejecutable no existe en el directorio de la aplicación")).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cerrar" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("renders nothing when there is no service", () => {
    render(<EditServiceDialog vm="vm1" service={null} onClose={vi.fn()} onDone={vi.fn()} />);

    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
