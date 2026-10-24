import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "./types";
import { UserKeyForm } from "./UserKeyForm";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };
const KEY = "-----BEGIN OPENSSH PRIVATE KEY-----\nclave-de-prueba\n-----END OPENSSH PRIVATE KEY-----\n";
const UNAVAILABLE = "La llave ya no está disponible, genere una nueva";
const GENERATED = "Llave de root generada para vm1";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function keyResponse() {
  return new Response(KEY, {
    status: 200,
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Content-Disposition": 'attachment; filename="id_ed25519_vm1"',
    },
  });
}

// jsdom no implementa URL.createObjectURL ni la navegación de un <a download>: se simulan
// y se registra el nombre del archivo y el contenido que se "descargó".
let downloads: { filename: string; blob: Blob }[] = [];
const blobs = new Map<string, Blob>();

beforeEach(() => {
  downloads = [];
  blobs.clear();
  URL.createObjectURL = vi.fn((blob: Blob) => {
    const url = `blob:llave-${blobs.size}`;
    blobs.set(url, blob);
    return url;
  });
  URL.revokeObjectURL = vi.fn();
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    downloads.push({ filename: this.download, blob: blobs.get(this.href)! });
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("UserKeyForm", () => {
  it("AC-9 shows download button and usage message", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse(200, { message: GENERATED }))
      .mockResolvedValueOnce(keyResponse());
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<UserKeyForm vm={vm1} onDone={onDone} />);

    expect(screen.queryByRole("button", { name: "Descargar llave privada" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Generar llave" }));

    expect(await screen.findByText(GENERATED)).toBeInTheDocument();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0];
    expect(url).toBe("/api/prepare/vms/vm1/user-key");
    expect(init?.method).toBe("POST");
    expect(onDone).toHaveBeenCalled();

    const usage = screen.getByText("Uso correcto de la llave privada").closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("chmod 600 id_ed25519_vm1");
    expect(usage).toHaveTextContent(/no la comparta/i);
    expect(usage).toHaveTextContent("ssh -i id_ed25519_vm1 root@192.168.1.50");
    expect(usage).not.toHaveTextContent("127.0.0.1");
    expect(usage).not.toHaveTextContent(" -p ");

    await user.click(screen.getByRole("button", { name: "Descargar llave privada" }));

    expect(fetchSpy.mock.calls[1][0]).toBe("/api/prepare/vms/vm1/user-key/download");
    expect(await screen.findByText(/Llave descargada/)).toBeInTheDocument();
    expect(downloads).toHaveLength(1);
    expect(downloads[0].filename).toBe("id_ed25519_vm1");
    expect(await downloads[0].blob.text()).toBe(KEY);
  });

  it("AC-10 second download shows unavailable", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse(200, { message: GENERATED }))
      .mockResolvedValueOnce(keyResponse())
      .mockResolvedValueOnce(jsonResponse(410, { error: UNAVAILABLE }));
    const user = userEvent.setup();
    render(<UserKeyForm vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Generar llave" }));
    await user.click(await screen.findByRole("button", { name: "Descargar llave privada" }));
    expect(await screen.findByText(/Llave descargada/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Descargar llave privada" }));

    expect(await screen.findByText(UNAVAILABLE)).toBeInTheDocument();
    expect(downloads).toHaveLength(1);
  });

  it("AC-11 expired key shows unavailable", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse(200, { message: GENERATED }))
      .mockResolvedValueOnce(jsonResponse(410, { error: UNAVAILABLE }));
    const user = userEvent.setup();
    render(<UserKeyForm vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Generar llave" }));
    await user.click(await screen.findByRole("button", { name: "Descargar llave privada" }));

    expect(await screen.findByText(UNAVAILABLE)).toBeInTheDocument();
    expect(screen.getByText("La operación falló")).toBeInTheDocument();
    expect(downloads).toHaveLength(0);
  });

  it("AC-9 usage message shows a placeholder when the VM has no IP", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: GENERATED }));
    const user = userEvent.setup();
    render(<UserKeyForm vm={{ ...vm1, ip: "" }} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Generar llave" }));

    const usage = (await screen.findByText("Uso correcto de la llave privada")).closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("ssh -i id_ed25519_vm1 root@<IP de la VM>");
  });

  it("AC-27 usage message shows the ssh -J command for a server behind a load balancer", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: GENERATED }));
    const server: Vm = { ...vm1, name: "web-a", ip: "10.10.1.11", role: "server", lb: "lb1", lb_ip: "192.168.1.60" };
    const user = userEvent.setup();
    render(<UserKeyForm vm={server} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Generar llave" }));

    const usage = (await screen.findByText("Uso correcto de la llave privada")).closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("ssh -J jump@192.168.1.60 -i id_ed25519_web-a root@10.10.1.11");
    expect(usage).toHaveTextContent("chmod 600 id_ed25519_web-a");
  });

  it("AC-12 regenerate asks for confirmation", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { message: GENERATED }));
    const user = userEvent.setup();
    render(<UserKeyForm vm={{ ...vm1, user_key_generated: true }} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Generar llave" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent(/la llave anterior dejará de funcionar/i);
    expect(fetchSpy).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText(GENERATED)).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/prepare/vms/vm1/user-key");
  });

  it("AC-5 key form disabled without VM", () => {
    render(<UserKeyForm vm={null} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Generar llave" })).toBeDisabled();
    expect(screen.getByText("Seleccione una VM para continuar")).toBeInTheDocument();
  });

  it("AC-43 shows no-IP message", async () => {
    const noIp = "La VM no tiene dirección IP; verifique que esté encendida";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: noIp }));
    const user = userEvent.setup();
    render(<UserKeyForm vm={{ ...vm1, state: "poweroff", ip: "" }} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Generar llave" }));

    expect(await screen.findByText(noIp)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveClass("text-destructive");
    expect(screen.queryByRole("button", { name: "Descargar llave privada" })).not.toBeInTheDocument();
  });
});
