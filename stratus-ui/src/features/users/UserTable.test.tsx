import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { Vm } from "@/features/prepare/types";

import { UserTable } from "./UserTable";

const vm1: Vm = { name: "vm1", state: "running", ip: "192.168.1.50", user_key_generated: false, role: "vm", lb: "", lb_ip: "" };
const USERS = [
  { username: "ana", uid: 1001, sudo: true, ssh_key: true },
  { username: "luis", uid: 1002, sudo: false, ssh_key: false },
];

afterEach(() => {
  vi.restoreAllMocks();
});

describe("UserTable", () => {
  it("AC-1 shows each user with Sí/No", () => {
    render(<UserTable users={USERS} vm={vm1} onDone={vi.fn()} />);

    const headers = screen.getAllByRole("columnheader").map((header) => header.textContent);
    expect(headers).toEqual(["Usuario", "UID", "Sudo", "Llave", "Acciones"]);

    // Las cuatro primeras celdas de cada fila son los datos; la última es el botón de eliminar.
    const rows = screen.getAllByRole("row").slice(1);
    const text = rows.map((row) =>
      within(row)
        .getAllByRole("cell")
        .slice(0, 4)
        .map((cell) => cell.textContent)
        .join(" "),
    );
    expect(text).toEqual(["ana 1001 Sí Sí", "luis 1002 No No"]);
  });

  it("AC-2 shows an empty state when there are no users", () => {
    render(<UserTable users={[]} vm={vm1} onDone={vi.fn()} />);

    expect(screen.getByText("Sin usuarios regulares")).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("AC-16 the delete button opens the dialog naming the user and sends nothing yet", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const user = userEvent.setup();
    render(<UserTable users={USERS} vm={vm1} onDone={vi.fn()} />);

    await user.click(screen.getByRole("button", { name: "Eliminar luis" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("luis");
    expect(dialog).not.toHaveTextContent("ana");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("AC-16 a confirmed delete calls onDone", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ message: "Usuario luis eliminado de vm1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    const onDone = vi.fn();
    const user = userEvent.setup();
    render(<UserTable users={USERS} vm={vm1} onDone={onDone} />);

    await user.click(screen.getByRole("button", { name: "Eliminar luis" }));
    await user.click(await screen.findByRole("button", { name: "Confirmar" }));

    expect(await screen.findByText("Usuario luis eliminado de vm1")).toBeInTheDocument();
    expect(onDone).toHaveBeenCalledTimes(1);
  });
});
