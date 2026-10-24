import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "./types";
import { VmSelector } from "./VmSelector";

const vms: Vm[] = [
  { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
  { name: "vm2", state: "unknown", ip: "", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
];

describe("VmSelector", () => {
  it("AC-3 lists each VM with its state and IP", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<VmSelector vms={vms} value={null} onChange={onChange} />);

    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));

    expect(await screen.findByRole("option", { name: "vm1 - Encendida - 192.168.1.50" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "vm2 - Desconocido - -" })).toBeInTheDocument();

    await user.click(screen.getByRole("option", { name: "vm1 - Encendida - 192.168.1.50" }));
    expect(onChange).toHaveBeenCalledWith("vm1");
  });

  it("AC-42 shows - for a VM without IP", async () => {
    const mixed: Vm[] = [
      { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
      { name: "vm2", state: "poweroff", ip: "", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
    ];
    const user = userEvent.setup();
    render(<VmSelector vms={mixed} value={null} onChange={vi.fn()} />);

    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));

    expect(await screen.findByRole("option", { name: "vm1 - Encendida - 192.168.1.50" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "vm2 - Apagada - -" })).toBeInTheDocument();
  });

  it("AC-9 labels a load balancer with Balanceador", async () => {
    const withLb: Vm[] = [
      { name: "lb1", state: "running", ip: "192.168.1.60", user_key_generated: false, role: "lb", lb: "", lb_ip: "" },
      { name: "web-a", state: "running", ip: "10.10.1.11", user_key_generated: false, role: "server", lb: "lb1", lb_ip: "192.168.1.60" },
      { name: "vm1", state: "poweroff", ip: "", user_key_generated: false, role: "vm", lb: "", lb_ip: "" },
    ];
    const user = userEvent.setup();
    render(<VmSelector vms={withLb} value={null} onChange={vi.fn()} />);

    await user.click(screen.getByRole("combobox", { name: "VM de trabajo" }));

    expect(await screen.findByRole("option", { name: "lb1 - Encendida - 192.168.1.60 - Balanceador" })).toBeInTheDocument();
    // Un servidor y una VM común no llevan la etiqueta.
    expect(screen.getByRole("option", { name: "web-a - Encendida - 10.10.1.11" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "vm1 - Apagada - -" })).toBeInTheDocument();
  });
});
