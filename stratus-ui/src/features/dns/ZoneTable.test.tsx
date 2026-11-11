import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { ZoneRecord } from "./types";
import { ZoneTable } from "./ZoneTable";

const SOA = "dns1.stratus.lan. hostmaster.stratus.lan. 1731300000 3600 600 86400 60";
const RECORDS: ZoneRecord[] = [
  { type: "SOA", name: "stratus.lan", value: SOA, ttl: 60 },
  { type: "NS", name: "stratus.lan", value: "dns1.stratus.lan.", ttl: 60 },
  { type: "A", name: "dns1.stratus.lan", value: "192.168.1.53", ttl: 60 },
  { type: "A", name: "web-a.stratus.lan", value: "192.168.1.40", ttl: 60 },
];

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// Celdas de cada fila de datos, en el orden de las columnas.
function rows(): string[][] {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((row) =>
      within(row)
        .getAllByRole("cell")
        .map((cell) => cell.textContent ?? ""),
    );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ZoneTable", () => {
  it("AC-66 lists type, name, value and TTL of each record, with copy buttons on A values", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(200, { records: RECORDS }));
    render(<ZoneTable version={0} />);

    expect(await screen.findByText("web-a.stratus.lan")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledWith("/api/dns/zone", expect.objectContaining({ method: "GET" }));
    for (const header of ["Tipo", "Nombre", "Valor", "TTL"]) {
      expect(screen.getByRole("columnheader", { name: header })).toBeInTheDocument();
    }
    expect(rows()).toEqual([
      ["SOA", "stratus.lan", SOA, "60"],
      ["NS", "stratus.lan", "dns1.stratus.lan.", "60"],
      ["A", "dns1.stratus.lan", "192.168.1.53", "60"],
      ["A", "web-a.stratus.lan", "192.168.1.40", "60"],
    ]);
    expect(screen.getByRole("button", { name: "Copiar 192.168.1.53" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copiar 192.168.1.40" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Copiar dns1/ })).not.toBeInTheDocument();
  });

  it("AC-67 Recargar reads the zone again and shows the new row", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse(200, { records: RECORDS.slice(0, 3) }))
      .mockResolvedValueOnce(jsonResponse(200, { records: RECORDS }));
    const user = userEvent.setup();
    render(<ZoneTable version={0} />);
    expect(await screen.findByText("dns1.stratus.lan")).toBeInTheDocument();
    expect(screen.queryByText("web-a.stratus.lan")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Recargar" }));

    expect(await screen.findByText("web-a.stratus.lan")).toBeInTheDocument();
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });

  it("AC-67 a new version reads the zone again", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async () => jsonResponse(200, { records: RECORDS }));
    const { rerender } = render(<ZoneTable version={0} />);
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));

    rerender(<ZoneTable version={1} />);

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2));
  });

  it("AC-68 shows the error when the DNS server is off", async () => {
    const text = "El servidor DNS está apagado; enciéndalo para ver la zona";
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(409, { error: text }));
    render(<ZoneTable version={0} />);

    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("REQ-47 shows a message for an empty zone and disables Recargar while reading", async () => {
    let resolve: (r: Response) => void = () => {};
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise((r) => (resolve = r)));
    render(<ZoneTable version={0} />);

    // Mientras lee, el spinner suma "Cargando" al nombre del botón.
    expect(screen.getByRole("button", { name: /Recargar/ })).toBeDisabled();
    resolve(jsonResponse(200, { records: [] }));

    expect(await screen.findByText("La zona no tiene registros")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Recargar" })).toBeEnabled();
  });
});
