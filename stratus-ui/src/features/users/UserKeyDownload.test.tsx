import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "@/features/prepare/types";

import { UserKeyDownload } from "./UserKeyDownload";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };
const KEY = "-----BEGIN OPENSSH PRIVATE KEY-----\nclave-de-prueba\n-----END OPENSSH PRIVATE KEY-----\n";
const UNAVAILABLE = "La llave ya no está disponible; elimine y vuelva a crear el usuario para obtener una nueva";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// jsdom no implementa URL.createObjectURL ni la navegación de un <a download>: se simulan.
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

describe("UserKeyDownload", () => {
  it("AC-4 shows the usage message with the VM IPv4", () => {
    render(<UserKeyDownload vm={vm1} username="ana" />);

    const usage = screen.getByText("Uso correcto de la llave privada").closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("chmod 600 id_ed25519_vm1_ana");
    expect(usage).toHaveTextContent(/no la comparta/i);
    expect(usage).toHaveTextContent("ssh -i id_ed25519_vm1_ana ana@192.168.1.50");
  });

  it("AC-4 usage message shows a placeholder when the VM has no IP", () => {
    render(<UserKeyDownload vm={{ ...vm1, ip: "" }} username="ana" />);

    const usage = screen.getByText("Uso correcto de la llave privada").closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("ssh -i id_ed25519_vm1_ana ana@<IP de la VM>");
  });

  it("AC-27 usage message shows the ssh -J command for a server behind a load balancer", () => {
    const server: Vm = { ...vm1, name: "web-a", ip: "10.10.1.11", role: "server", lb: "lb1", lb_ip: "192.168.1.60" };
    render(<UserKeyDownload vm={server} username="ana" />);

    const usage = screen.getByText("Uso correcto de la llave privada").closest('[data-slot="alert"]');
    expect(usage).toHaveTextContent("ssh -J jump@192.168.1.60 -i id_ed25519_web-a_ana ana@10.10.1.11");
    expect(usage).toHaveTextContent("chmod 600 id_ed25519_web-a_ana");
  });

  it("AC-13 downloads the key as id_ed25519_<vm>_<username>", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(KEY, { status: 200, headers: { "Content-Type": "text/plain; charset=utf-8" } }),
    );
    const user = userEvent.setup();
    render(<UserKeyDownload vm={vm1} username="ana" />);

    await user.click(screen.getByRole("button", { name: "Descargar llave privada" }));

    expect(await screen.findByText(/Llave descargada/)).toBeInTheDocument();
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/users/vms/vm1/users/ana/key/download");
    expect(downloads).toHaveLength(1);
    expect(downloads[0].filename).toBe("id_ed25519_vm1_ana");
    expect(await downloads[0].blob.text()).toBe(KEY);
  });

  it("AC-13 shows the unavailable message on 410", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(410, { error: UNAVAILABLE }));
    const user = userEvent.setup();
    render(<UserKeyDownload vm={vm1} username="ana" />);

    await user.click(screen.getByRole("button", { name: "Descargar llave privada" }));

    expect(await screen.findByText(UNAVAILABLE)).toBeInTheDocument();
    expect(downloads).toHaveLength(0);
  });
});
