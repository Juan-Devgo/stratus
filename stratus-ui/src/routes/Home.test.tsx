import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { App } from "../App";

describe("Home", () => {
  it("AC-36 Backends HAProxy navigates and no card shows Próximamente", () => {
    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    const cards = within(screen.getByRole("list", { name: "Módulos" })).getAllByRole("listitem");
    expect(cards).toHaveLength(4);

    expect(screen.getByRole("link", { name: /Preparar sistema/ })).toHaveAttribute("href", "/prepare");
    expect(screen.getByRole("link", { name: /Gestión de usuarios/ })).toHaveAttribute("href", "/users");
    expect(screen.getByRole("link", { name: /Servicios systemd/ })).toHaveAttribute("href", "/services");
    expect(screen.getByRole("link", { name: /Backends HAProxy/ })).toHaveAttribute("href", "/haproxy");
    expect(
      screen.getByText("Crear balanceadores HAProxy, asignar servidores y ver su estado."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Próximamente")).not.toBeInTheDocument();
  });
});
