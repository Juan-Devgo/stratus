import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { LbTable } from "./LbTable";
import type { Lb } from "./types";

const lbs: Lb[] = [
  { name: "lb1", state: "running", ip: "192.168.1.60", subnet: "10.10.1.0/24", frontend_port: 8080, server_count: 2 },
  { name: "lb2", state: "poweroff", ip: "", subnet: "10.10.2.0/24", frontend_port: 80, server_count: 0 },
];

describe("LbTable", () => {
  it("AC-9 lists name, state, IP, subnet, port and server count", () => {
    render(<LbTable lbs={lbs} selected={null} onSelect={vi.fn()} />);

    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent);
    expect(headers.slice(0, 6)).toEqual(["Nombre", "Estado", "IP", "Subred", "Puerto", "Servidores"]);

    const [first, second] = screen.getAllByRole("row").slice(1);
    for (const text of ["lb1", "Encendida", "192.168.1.60", "10.10.1.0/24", "8080", "2"]) {
      expect(within(first).getByText(text)).toBeInTheDocument();
    }
    // Apagado: la IP se muestra como "-".
    for (const text of ["lb2", "Apagada", "-", "10.10.2.0/24", "80", "0"]) {
      expect(within(second).getByText(text)).toBeInTheDocument();
    }
  });

  it("AC-9 selecting a row calls onSelect with its name", async () => {
    const onSelect = vi.fn();
    const user = userEvent.setup();
    render(<LbTable lbs={lbs} selected={null} onSelect={onSelect} />);

    await user.click(screen.getByRole("button", { name: "Seleccionar lb2" }));

    expect(onSelect).toHaveBeenCalledWith("lb2");
  });

  it("AC-9 marks the selected row", () => {
    render(<LbTable lbs={lbs} selected="lb1" onSelect={vi.fn()} />);

    const [first, second] = screen.getAllByRole("row").slice(1);
    expect(first).toHaveAttribute("data-state", "selected");
    expect(second).not.toHaveAttribute("data-state", "selected");
    expect(screen.getByRole("button", { name: "Seleccionar lb1" })).toHaveAttribute("aria-pressed", "true");
  });

  it("AC-9 shows an empty state without load balancers", () => {
    render(<LbTable lbs={[]} selected={null} onSelect={vi.fn()} />);

    expect(screen.getByText("Sin balanceadores")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});
