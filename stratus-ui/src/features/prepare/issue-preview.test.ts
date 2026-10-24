import { composeIssue, ISSUE_FIELDS } from "./issue-preview";

describe("composeIssue", () => {
  it("AC-14 composeIssue orders message then fields with labels", () => {
    // Los escapes quedan literales: agetty los expande al mostrar /etc/issue.
    expect(composeIssue("Hola", ["users", "hostname"]).split("\n")).toEqual(["Hola", "Host: \\n", "Usuarios: \\U"]);
  });

  it("AC-14 composeIssue uses the REQ-26 labels and order for every field", () => {
    const all = ISSUE_FIELDS.map((field) => field.key).reverse();

    expect(composeIssue("", all)).toBe(
      [
        "Host: \\n",
        "Fecha: \\d",
        "Hora: \\t",
        "IPv4: \\4",
        "Sistema: \\s",
        "Kernel: \\r",
        "Arquitectura: \\m",
        "Terminal: \\l",
        "Usuarios: \\U",
      ].join("\n"),
    );
  });

  it("AC-14 composeIssue keeps the message line breaks", () => {
    expect(composeIssue("Configuración\nde producción", ["date"])).toBe("Configuración\nde producción\nFecha: \\d");
    expect(composeIssue("", [])).toBe("");
  });
});
