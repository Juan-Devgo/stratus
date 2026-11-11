import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CreateVmDialog } from "./CreateVmDialog";
import { CreateVmStatus } from "./CreateVmStatus";

describe("CreateVmDialog", () => {
  it("shows the card with the form when open", () => {
    render(<CreateVmDialog open onOpenChange={vi.fn()} pending={false} result={null} onSubmit={vi.fn()} />);

    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/Crea un clon de Debian 13 sobre el disco base compartido/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Nombre")).toBeInTheDocument();
  });

  it("renders nothing when closed", () => {
    render(<CreateVmDialog open={false} onOpenChange={vi.fn()} pending={false} result={null} onSubmit={vi.fn()} />);

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("asks to close from the close button", async () => {
    const onOpenChange = vi.fn();
    const user = userEvent.setup();
    render(<CreateVmDialog open onOpenChange={onOpenChange} pending result={null} onSubmit={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Cerrar" }));

    expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything());
  });
});

describe("CreateVmStatus", () => {
  it("renders nothing without an operation", () => {
    const { container } = render(<CreateVmStatus name={null} pending={false} result={null} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("shows progress with the VM name while pending", () => {
    render(<CreateVmStatus name="vm1" pending result={null} />);

    expect(screen.getByRole("status", { name: "Cargando" })).toBeInTheDocument();
    expect(screen.getByText(/Creando la VM vm1/)).toBeInTheDocument();
  });

  it("shows the final message once finished", () => {
    render(<CreateVmStatus name="vm1" pending={false} result={{ ok: true, text: "VM vm1 creada y encendida" }} />);

    expect(screen.getByText("VM vm1 creada y encendida")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
