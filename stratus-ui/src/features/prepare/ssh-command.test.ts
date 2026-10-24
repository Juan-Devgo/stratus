import type { Vm } from "./types";
import { sshCommand } from "./ssh-command";

const base: Vm = { name: "web-a", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };

describe("sshCommand", () => {
  it("AC-27 gives the ssh -J command for a server behind a load balancer", () => {
    const server: Vm = { ...base, ip: "10.10.1.11", role: "server", lb: "lb1", lb_ip: "192.168.1.60" };

    expect(sshCommand(server, "id_ed25519_web-a", "root")).toBe(
      "ssh -J jump@192.168.1.60 -i id_ed25519_web-a root@10.10.1.11",
    );
    expect(sshCommand(server, "id_ed25519_web-a_ana", "ana")).toBe(
      "ssh -J jump@192.168.1.60 -i id_ed25519_web-a_ana ana@10.10.1.11",
    );
  });

  it("AC-27 gives the direct command for a plain VM", () => {
    expect(sshCommand(base, "id_ed25519_vm1", "root")).toBe("ssh -i id_ed25519_vm1 root@192.168.1.50");
  });

  it("AC-27 a load balancer is reached directly, without the jump", () => {
    const lb: Vm = { ...base, name: "lb1", ip: "192.168.1.60", role: "lb" };

    expect(sshCommand(lb, "id_ed25519_lb1", "root")).toBe("ssh -i id_ed25519_lb1 root@192.168.1.60");
  });

  it("AC-27 shows placeholders while an address is unknown", () => {
    expect(sshCommand({ ...base, ip: "" }, "key", "root")).toBe("ssh -i key root@<IP de la VM>");
    const server: Vm = { ...base, ip: "", role: "server", lb: "lb1", lb_ip: "" };

    expect(sshCommand(server, "key", "root")).toBe("ssh -J jump@<IP del balanceador> -i key root@<IP interna>");
  });
});
