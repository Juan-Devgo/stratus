import { render, screen } from "@testing-library/react";

import { StateWarning } from "./StateWarning";

describe("StateWarning", () => {
  it("AC-14 shows the warning for non-active states", () => {
    const { rerender } = render(<StateWarning name="api" state="failed" />);
    expect(screen.getByRole("alert")).toHaveTextContent("El servicio api quedó en estado failed; revise Estado y Logs");

    rerender(<StateWarning name="api" state="activating (auto-restart)" />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "El servicio api quedó en estado activating (auto-restart); revise Estado y Logs",
    );

    // desconocido es lo que informa el script si no pudo leer el estado (REQ-15).
    rerender(<StateWarning name="api" state="desconocido" />);
    expect(screen.getByRole("alert")).toHaveTextContent("El servicio api quedó en estado desconocido");

    rerender(<StateWarning name="api" state="active" />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    rerender(<StateWarning name="api" />);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
