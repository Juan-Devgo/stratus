import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { ServiceRow } from "./types";
import { ServiceTable } from "./ServiceTable";

const SERVICES: ServiceRow[] = [
  {
    name: "demian-lee",
    description: "Servidor de producción",
    active_state: "active",
    enabled_state: "enabled",
    restart: "on-failure",
    restart_sec: 5,
    executable: "demian-lee",
    args: "",
    env_keys: ["PORT", "SERVER_NAME", "ADMIN_PASSWORD"],
  },
  {
    name: "api",
    description: "API interna",
    active_state: "inactive",
    enabled_state: "disabled",
    restart: "always",
    restart_sec: 10,
    executable: "bin/server",
    args: "--port 9090",
    env_keys: [],
  },
];

describe("ServiceTable", () => {
  it("AC-15 shows each service row", () => {
    render(<ServiceTable services={SERVICES} busy={false} onAction={vi.fn()} onStatus={vi.fn()} onLogs={vi.fn()} onEdit={vi.fn()} onRedeploy={vi.fn()} onDelete={vi.fn()} />);

    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent);
    expect(headers).toEqual(["Servicio", "Descripción", "Estado", "Habilitado", "Restart", "RestartSec", "Acciones"]);

    const rows = screen.getAllByRole("row").slice(1);
    const text = rows.map((row) =>
      within(row)
        .getAllByRole("cell")
        .slice(0, 6)
        .map((cell) => cell.textContent)
        .join(" | "),
    );
    expect(text).toEqual([
      "demian-lee | Servidor de producción | active | enabled | on-failure | 5",
      "api | API interna | inactive | disabled | always | 10",
    ]);
  });

  it("AC-15 shows an empty state when there are no services", () => {
    render(<ServiceTable services={[]} busy={false} onAction={vi.fn()} onStatus={vi.fn()} onLogs={vi.fn()} onEdit={vi.fn()} onRedeploy={vi.fn()} onDelete={vi.fn()} />);

    expect(screen.getByText("Sin servicios de Stratus")).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("AC-18 row buttons call each action", async () => {
    const onAction = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(<ServiceTable services={SERVICES} busy={false} onAction={onAction} onStatus={vi.fn()} onLogs={vi.fn()} onEdit={vi.fn()} onRedeploy={vi.fn()} onDelete={vi.fn()} />);

    const labels: [string, string][] = [
      ["Iniciar", "start"],
      ["Detener", "stop"],
      ["Reiniciar", "restart"],
      ["Habilitar", "enable"],
      ["Deshabilitar", "disable"],
    ];
    for (const [label, action] of labels) {
      await user.click(screen.getByRole("button", { name: `${label} api` }));
      expect(onAction).toHaveBeenLastCalledWith("api", action);
    }
    expect(onAction).toHaveBeenCalledTimes(5);
    // Cada servicio tiene sus cinco botones.
    expect(screen.getByRole("button", { name: "Iniciar demian-lee" })).toBeInTheDocument();
  });

  it("AC-37 row buttons are disabled while an operation runs", () => {
    render(<ServiceTable services={SERVICES} busy onAction={vi.fn()} onStatus={vi.fn()} onLogs={vi.fn()} onEdit={vi.fn()} onRedeploy={vi.fn()} onDelete={vi.fn()} />);

    const buttons = screen.getAllByRole("button");
    expect(buttons).toHaveLength(20);
    for (const button of buttons) {
      expect(button).toBeDisabled();
    }
  });

  it("AC-21 AC-22 Estado and Logs buttons call onStatus and onLogs", async () => {
    const onStatus = vi.fn();
    const onLogs = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(<ServiceTable services={SERVICES} busy={false} onAction={vi.fn()} onStatus={onStatus} onLogs={onLogs} onEdit={vi.fn()} onRedeploy={vi.fn()} onDelete={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Estado api" }));
    await user.click(screen.getByRole("button", { name: "Logs demian-lee" }));

    expect(onStatus).toHaveBeenCalledExactlyOnceWith("api");
    expect(onLogs).toHaveBeenCalledExactlyOnceWith("demian-lee");
  });

  it("AC-23 the Editar button passes the whole row to onEdit", async () => {
    const onEdit = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(
      <ServiceTable services={SERVICES} busy={false} onAction={vi.fn()} onStatus={vi.fn()} onLogs={vi.fn()} onEdit={onEdit} onRedeploy={vi.fn()} onDelete={vi.fn()} />,
    );

    await user.click(screen.getByRole("button", { name: "Editar api" }));

    expect(onEdit).toHaveBeenCalledExactlyOnceWith(SERVICES[1]);
  });

  it("AC-38 the Redesplegar button passes the whole row to onRedeploy", async () => {
    const onRedeploy = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(
      <ServiceTable
        services={SERVICES}
        busy={false}
        onAction={vi.fn()}
        onStatus={vi.fn()}
        onLogs={vi.fn()}
        onEdit={vi.fn()}
        onRedeploy={onRedeploy}
        onDelete={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Redesplegar demian-lee" }));

    expect(onRedeploy).toHaveBeenCalledExactlyOnceWith(SERVICES[0]);
  });

  it("AC-28 the Eliminar button passes the service name to onDelete", async () => {
    const onDelete = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(
      <ServiceTable
        services={SERVICES}
        busy={false}
        onAction={vi.fn()}
        onStatus={vi.fn()}
        onLogs={vi.fn()}
        onEdit={vi.fn()}
        onRedeploy={vi.fn()}
        onDelete={onDelete}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Eliminar api" }));

    expect(onDelete).toHaveBeenCalledExactlyOnceWith("api");
  });
});
