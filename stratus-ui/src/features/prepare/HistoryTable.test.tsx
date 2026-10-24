import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { HistoryTable } from "./HistoryTable";
import type { OperationRow } from "./types";

function row(overrides: Partial<OperationRow>): OperationRow {
  return {
    id: 1,
    operation: "start_vm",
    vm: "vm1",
    command: "prepare/start-vm.sh",
    args: ["vm1"],
    exit_code: 0,
    stderr: "",
    result: "ok",
    created_at: "2026-10-21T15:00:00Z",
    ...overrides,
  };
}

// Fila de la tabla que contiene el texto (la VM es única en cada prueba).
function rowOf(text: string): HTMLElement {
  const tr = screen.getByText(text).closest("tr");
  if (!tr) {
    throw new Error(`no hay una fila con ${text}`);
  }
  return tr;
}

describe("HistoryTable", () => {
  it("AC-22 lists rows newest first and expands stderr", async () => {
    // 50 filas como las entrega el backend: id 50 primero.
    const operations = Array.from({ length: 50 }, (_, i) =>
      row({ id: 50 - i, vm: `vm-${50 - i}`, stderr: `stderr-${50 - i}` }),
    );
    const user = userEvent.setup();
    render(<HistoryTable operations={operations} />);

    const names = screen.getAllByText(/^vm-\d+$/).map((cell) => cell.textContent);
    expect(names).toEqual(operations.map((op) => op.vm));
    expect(screen.queryByText("stderr-50")).not.toBeInTheDocument();

    await user.click(within(rowOf("vm-50")).getByRole("button", { name: "Ver stderr" }));

    expect(await screen.findByText("stderr-50")).toBeInTheDocument();
    expect(screen.queryByText("stderr-49")).not.toBeInTheDocument();
  });

  it("AC-8 failed row shows exit code and stderr", async () => {
    const user = userEvent.setup();
    render(
      <HistoryTable
        operations={[row({ operation: "install_package", command: "prepare/install-package.sh", exit_code: 5, stderr: "boom", result: "error" })]}
      />,
    );

    const tr = within(rowOf("vm1"));
    expect(tr.getByText("Fallo")).toBeInTheDocument();
    expect(tr.getByText("5")).toBeInTheDocument();

    await user.click(tr.getByRole("button", { name: "Ver stderr" }));

    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(screen.getByText("Código de salida: 5")).toBeInTheDocument();
  });

  it("AC-37 placeholder failure row shows code 99 and its stderr", async () => {
    const user = userEvent.setup();
    render(<HistoryTable operations={[row({ exit_code: 99, stderr: "Operación no implementada", result: "error" })]} />);

    await user.click(screen.getByRole("button", { name: "Ver stderr" }));

    expect(await screen.findByText("Operación no implementada")).toBeInTheDocument();
    expect(screen.getByText("Código de salida: 99")).toBeInTheDocument();
  });

  it("AC-22 shows an empty state when there are no operations", () => {
    render(<HistoryTable operations={[]} />);

    expect(screen.getByText("Sin operaciones registradas")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});
