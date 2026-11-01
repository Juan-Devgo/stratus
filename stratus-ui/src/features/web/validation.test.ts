import { createWebErrors, validatePageFile } from "./validation";

const TYPE_MESSAGE = "Seleccione el tipo de servidor web";

// Archivo del tamaño pedido sin reservar la memoria: solo cambia size.
function sizedFile(name: string, size: number): File {
  const file = new File(["x"], name);
  Object.defineProperty(file, "size", { value: size });
  return file;
}

describe("createWebErrors", () => {
  it("AC-2 adds the type error to the 001 field errors", () => {
    const errors = createWebErrors({ name: "Web_1", memory_mb: 256, cpus: 5, type: "" });
    expect(errors.name).toMatch(/El nombre de la VM debe tener/);
    expect(errors.memory_mb).toBe("La memoria debe estar entre 512 y 8192 MB");
    expect(errors.cpus).toBe("El número de CPUs debe estar entre 1 y 4");
    expect(errors.type).toBe(TYPE_MESSAGE);
  });

  it("AC-1 accepts valid fields with apache or nginx", () => {
    for (const type of ["apache", "nginx"] as const) {
      expect(createWebErrors({ name: "web-a", memory_mb: 1024, cpus: 1, type }), type).toEqual({});
    }
  });
});

describe("validatePageFile", () => {
  it("AC-13 rejects site.zip and a 201 MB file with the 003 texts", () => {
    expect(validatePageFile(new File(["x"], "site.zip"))).toBe("El archivo debe ser .tar.gz");
    expect(validatePageFile(sizedFile("site.tar.gz", 201 * 1024 * 1024))).toBe("El archivo supera 200 MB");
  });

  it("AC-13 asks for a file and accepts a .tar.gz up to 200 MB", () => {
    expect(validatePageFile(null)).toBe("Seleccione el archivo .tar.gz de la página");
    expect(validatePageFile(sizedFile("site.tar.gz", 200 * 1024 * 1024))).toBeNull();
  });
});
