import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";

import { CopyButton } from "./CopyButton";
import { IpCell } from "./IpCell";

vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(toast.error).mockClear();
});

describe("CopyButton", () => {
  it("AC-63 copies the IP and shows the check icon", async () => {
    // userEvent.setup() instala un portapapeles falso en navigator.clipboard.
    const user = userEvent.setup();
    render(<CopyButton value="192.168.1.40" />);

    const button = screen.getByRole("button", { name: "Copiar 192.168.1.40" });
    expect(button).toHaveAttribute("data-copied", "false");
    await user.click(button);

    expect(await navigator.clipboard.readText()).toBe("192.168.1.40");
    expect(button).toHaveAttribute("data-copied", "true");
  });

  it("AC-64 shows a toast when the browser cannot copy", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error("denegado"));
    render(<CopyButton value="192.168.1.40" />);

    await user.click(screen.getByRole("button", { name: "Copiar 192.168.1.40" }));

    expect(toast.error).toHaveBeenCalledWith("No se pudo copiar 192.168.1.40");
    expect(screen.getByRole("button", { name: "Copiar 192.168.1.40" })).toHaveAttribute("data-copied", "false");
  });

  it("AC-63 copies without the clipboard API (plain HTTP from the LAN)", async () => {
    const user = userEvent.setup();
    // Sin contexto seguro el navegador no expone navigator.clipboard.
    vi.spyOn(navigator, "clipboard", "get").mockReturnValue(undefined as unknown as Clipboard);
    let copied = "";
    const exec = vi.fn(() => {
      copied = (document.activeElement as HTMLTextAreaElement).value;
      return true;
    });
    Object.defineProperty(document, "execCommand", { value: exec, configurable: true });
    render(<CopyButton value="10.10.1.11" />);

    await user.click(screen.getByRole("button", { name: "Copiar 10.10.1.11" }));

    expect(exec).toHaveBeenCalledWith("copy");
    expect(copied).toBe("10.10.1.11");
    expect(screen.getByRole("button", { name: "Copiar 10.10.1.11" })).toHaveAttribute("data-copied", "true");
  });
});

describe("IpCell", () => {
  it("AC-63 shows the IP with its copy button", () => {
    render(<IpCell ip="192.168.1.40" />);

    expect(screen.getByText("192.168.1.40")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copiar 192.168.1.40" })).toBeInTheDocument();
  });

  it("AC-63 shows - without a copy button", () => {
    render(<IpCell ip="" />);

    expect(screen.getByText("-")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
