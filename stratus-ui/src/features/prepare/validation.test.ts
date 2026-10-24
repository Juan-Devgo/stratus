import { validateCreateVm, validateHostname, validateIssue, validatePackage, validateVmName } from "./validation";

const valid = { name: "vm1", memory_mb: 2048, cpus: 2 };

describe("validateCreateVm", () => {
  it("AC-2 validateCreateVm rejects invalid values", () => {
    const invalid = [
      { ...valid, name: "VM1" },
      { ...valid, name: "-vm" },
      { ...valid, name: "a".repeat(33) },
      { ...valid, name: "" },
      { ...valid, name: "vm1; rm -rf /" },
      { ...valid, memory_mb: 511 },
      { ...valid, memory_mb: 8193 },
      { ...valid, cpus: 0 },
      { ...valid, cpus: 5 },
      { ...valid, cpus: 2.5 },
      { ...valid, memory_mb: Number.NaN },
    ];
    for (const input of invalid) {
      expect(validateCreateVm(input), JSON.stringify(input)).toEqual(expect.any(String));
    }

    expect(validateCreateVm(valid)).toBeNull();
    expect(validateCreateVm({ name: "a", memory_mb: 512, cpus: 1 })).toBeNull();
    expect(validateCreateVm({ name: "a".repeat(32), memory_mb: 8192, cpus: 4 })).toBeNull();
    expect(validateVmName("web-01")).toBeNull();
    expect(validateVmName("-vm")).toEqual(expect.any(String));
  });
});

describe("validatePackage", () => {
  it("AC-7 validatePackage rejects injection and empty", () => {
    for (const pkg of ["nginx; rm -rf /", "", "a", "Nginx", "-nginx", "nginx curl", "nginx\ncurl", "a".repeat(64)]) {
      expect(validatePackage(pkg), JSON.stringify(pkg)).toEqual(expect.any(String));
    }

    for (const pkg of ["nginx", "g++", "python3.13-venv", "vi", "a".repeat(63)]) {
      expect(validatePackage(pkg), pkg).toBeNull();
    }
  });
});

describe("validateHostname", () => {
  it("AC-18 validateHostname rejects invalid", () => {
    for (const hostname of ["Web_01", "-web", "web-", "a".repeat(64), "", "web.local", "web; reboot", "web\n01"]) {
      expect(validateHostname(hostname), JSON.stringify(hostname)).toEqual(expect.any(String));
    }

    for (const hostname of ["web-01", "a", "01", `a${"b".repeat(61)}c`]) {
      expect(validateHostname(hostname), hostname).toBeNull();
    }
  });
});

describe("validateIssue", () => {
  it("AC-15 validateIssue rejects > 500 chars", () => {
    expect(validateIssue({ message: "a".repeat(501), fields: ["hostname"] })).toEqual(expect.any(String));
    // "ñ" cuenta como un carácter, igual que en el backend.
    expect(validateIssue({ message: "ñ".repeat(500), fields: [] })).toBeNull();
  });

  it("AC-16 validateIssue rejects nothing selected", () => {
    expect(validateIssue({ message: "", fields: [] })).toEqual(expect.any(String));
    expect(validateIssue({ message: "", fields: ["users"] })).toBeNull();
    expect(validateIssue({ message: "Hola", fields: [] })).toBeNull();
  });

  it("AC-25 validateIssue allowlist", () => {
    for (const message of ["a\\b", "Host: \\n", "a\tb", "a\u0007b", "a\r\nb", "\u001b[31mrojo"]) {
      expect(validateIssue({ message, fields: [] }), JSON.stringify(message)).toEqual(expect.any(String));
    }

    expect(validateIssue({ message: "Configuración de producción\nlínea 2", fields: [] })).toBeNull();
  });
});
