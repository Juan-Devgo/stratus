import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { App } from "../App";

describe("Home", () => {
  it("AC-54 shows six enabled cards; Servidores web and DNS navigate to their screens", () => {
    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    const cards = within(screen.getByRole("list", { name: "Módulos" })).getAllByRole("listitem");
    expect(cards).toHaveLength(6);

    expect(screen.getByRole("link", { name: /Preparar sistema/ })).toHaveAttribute("href", "/prepare");
    expect(screen.getByRole("link", { name: /Gestión de usuarios/ })).toHaveAttribute("href", "/users");
    expect(screen.getByRole("link", { name: /Servicios systemd/ })).toHaveAttribute("href", "/services");
    expect(screen.getByRole("link", { name: /Backends HAProxy/ })).toHaveAttribute("href", "/haproxy");
    expect(screen.getByRole("link", { name: /Servidores web/ })).toHaveAttribute("href", "/web");
    expect(screen.getByRole("link", { name: /^DNS/ })).toHaveAttribute("href", "/dns");
    expect(screen.queryByText("Próximamente")).not.toBeInTheDocument();
  });
});
