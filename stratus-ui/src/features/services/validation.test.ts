import {
  createServiceErrors,
  editServiceErrors,
  redeployErrors,
  validateArchive,
  validateArgs,
  validateDescription,
  validateEnvRows,
  validateExecutable,
  validateRestart,
  validateRestartSec,
  validateServiceName,
} from "./validation";

const MIB = 1024 * 1024;

// Un archivo vacío al que se le cambia el tamaño informado, para no crear 201 MiB en memoria.
function fileOfSize(name: string, size: number): File {
  const file = new File([""], name);
  Object.defineProperty(file, "size", { value: size });
  return file;
}

const validCreate = {
  name: "demian-lee",
  description: "Servidor de producción",
  executable: "demian-lee",
  args: "",
  restart: "on-failure",
  restart_sec: "5",
  env: [{ key: "PORT", value: "8080" }],
  archive: fileOfSize("app.tar.gz", 1024),
};

describe("validation", () => {
  it("AC-3 rejects invalid service names", () => {
    for (const name of ["Demian", "1app", "app;rm", "app_x", "", "a".repeat(33), "-api", "api\n"]) {
      expect(validateServiceName(name), JSON.stringify(name)).toEqual(expect.any(String));
    }
    for (const name of ["demian-lee", "a", "a".repeat(32), "app-1"]) {
      expect(validateServiceName(name), name).toBeNull();
    }
  });

  it("AC-5 rejects a 201 MiB file and a file that is not .tar.gz", () => {
    expect(validateArchive(fileOfSize("app.tar.gz", 201 * MIB))).toBe("El archivo supera 200 MB");
    expect(validateArchive(fileOfSize("app.zip", 1024))).toBe("El archivo debe ser .tar.gz");
    expect(validateArchive(fileOfSize("app.gz", 1024))).toBe("El archivo debe ser .tar.gz");
    expect(validateArchive(null)).toEqual(expect.any(String));
    expect(validateArchive(fileOfSize("app.tar.gz", 200 * MIB))).toBeNull();
    expect(validateArchive(fileOfSize("app.tar.gz", 0))).toBeNull();
  });

  it("AC-7 rejects executables that leave /opt/<name> or are malformed", () => {
    for (const exe of ["../x", "/bin/sh", "a/./b", "a/../b", ".", "..", "", "a b", "a;b", "a".repeat(256)]) {
      expect(validateExecutable(exe), JSON.stringify(exe)).toEqual(expect.any(String));
    }
    for (const exe of ["demian-lee", "bin/server", "a.b/c_d-e", "a".repeat(255)]) {
      expect(validateExecutable(exe), exe).toBeNull();
    }
  });

  it("AC-8 rejects invalid descriptions, arguments, Restart and RestartSec", () => {
    for (const description of ["50%", "a\\b", 'di"ce', "dos\nlíneas", "tab\taquí", "", "a".repeat(101)]) {
      expect(validateDescription(description), JSON.stringify(description)).toEqual(expect.any(String));
    }
    for (const description of ["Servidor de producción ñ", "a".repeat(100), "ñ".repeat(100)]) {
      expect(validateDescription(description), description).toBeNull();
    }

    const tokens = (n: number) => Array(n).fill("a").join(" ");
    for (const args of ["a;b", "$HOME", '"x"', tokens(17), " a", "a ", "a  b", "a\nb", "x".repeat(129)]) {
      expect(validateArgs(args), JSON.stringify(args)).toEqual(expect.any(String));
    }
    for (const args of ["", "--port 8080", "--listen=0.0.0.0:80 a,b@c+d/e", tokens(16)]) {
      expect(validateArgs(args), args).toBeNull();
    }

    for (const restart of ["on-abort", "", "Always"]) {
      expect(validateRestart(restart), restart).toEqual(expect.any(String));
    }
    for (const restart of ["no", "always", "on-failure"]) {
      expect(validateRestart(restart), restart).toBeNull();
    }

    for (const sec of ["-1", "3601", "", "05", "1.5", "abc"]) {
      expect(validateRestartSec(sec), sec).toEqual(expect.any(String));
    }
    for (const sec of ["0", "5", "3600"]) {
      expect(validateRestartSec(sec), sec).toBeNull();
    }
  });

  it("AC-9 rejects invalid environment rows", () => {
    const row = (key: string, value = "v") => ({ key, value });
    const rowError = (rows: { key: string; value: string }[], index: number) => validateEnvRows(rows).rows[index];

    expect(rowError([row("port")], 0)).toEqual(expect.any(String));
    expect(rowError([row("1PORT")], 0)).toEqual(expect.any(String));
    expect(rowError([row("")], 0)).toEqual(expect.any(String));
    expect(rowError([row("A".repeat(65))], 0)).toEqual(expect.any(String));
    expect(rowError([row("PORT"), row("PORT")], 1)).toEqual(expect.any(String));
    expect(rowError([row("PORT"), row("PORT")], 0)).toBeNull();
    expect(rowError([row("K", "a\nb")], 0)).toEqual(expect.any(String));
    expect(rowError([row("K", "café")], 0)).toEqual(expect.any(String));
    expect(rowError([row("K", "x".repeat(1025))], 0)).toEqual(expect.any(String));

    const many = Array.from({ length: 51 }, (_, i) => row(`K${i}`));
    expect(validateEnvRows(many).list).toEqual(expect.any(String));

    // Filas válidas, incluido un valor vacío y los 50 permitidos.
    const valid = [row("PORT", "8080"), row("EMPTY", ""), row("PASS", 'a b "c" $HOME \\x'), row("K", "x".repeat(1024))];
    expect(validateEnvRows(valid)).toEqual({ rows: [null, null, null, null], list: null });
    expect(validateEnvRows(many.slice(0, 50)).list).toBeNull();
    expect(validateEnvRows([])).toEqual({ rows: [], list: null });
  });

  it("AC-9 error messages never repeat the value", () => {
    const { rows } = validateEnvRows([{ key: "port", value: "Secreta123" }]);
    expect(rows[0]).not.toContain("Secreta123");
    const long = validateEnvRows([{ key: "K", value: "Secreta123".repeat(200) }]).rows[0];
    expect(long).not.toContain("Secreta123");
  });

  it("AC-3 AC-5 AC-7 AC-8 AC-9 createServiceErrors reports each field", () => {
    expect(createServiceErrors(validCreate)).toEqual({});

    expect(Object.keys(createServiceErrors({ ...validCreate, name: "app_x" }))).toEqual(["name"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, description: "50%" }))).toEqual(["description"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, archive: fileOfSize("app.zip", 10) }))).toEqual(["archive"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, archive: null }))).toEqual(["archive"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, executable: "../x" }))).toEqual(["executable"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, args: "a;b" }))).toEqual(["args"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, restart: "on-abort" }))).toEqual(["restart"]);
    expect(Object.keys(createServiceErrors({ ...validCreate, restart_sec: "3601" }))).toEqual(["restart_sec"]);
    // Un error de fila usa la clave env_<índice>; el del conjunto, env.
    expect(Object.keys(createServiceErrors({ ...validCreate, env: [{ key: "port", value: "1" }] }))).toEqual(["env_0"]);
    const many = Array.from({ length: 51 }, (_, i) => ({ key: `K${i}`, value: "v" }));
    expect(Object.keys(createServiceErrors({ ...validCreate, env: many }))).toEqual(["env"]);
  });

  it("AC-7 AC-8 AC-9 editServiceErrors validates env rows only when they are replaced", () => {
    const edit = { description: "Servidor", executable: "demian-lee", args: "", restart: "always", restart_sec: "5", replace_env: false, env: [{ key: "port", value: "" }] };

    // Sin reemplazo las filas son solo las claves actuales: no se validan.
    expect(editServiceErrors(edit)).toEqual({});
    expect(Object.keys(editServiceErrors({ ...edit, replace_env: true }))).toEqual(["env_0"]);
    expect(editServiceErrors({ ...edit, replace_env: true, env: [{ key: "PORT", value: "9090" }] })).toEqual({});
    expect(Object.keys(editServiceErrors({ ...edit, executable: "/bin/sh", restart_sec: "-1" })).sort()).toEqual(["executable", "restart_sec"]);
  });

  it("AC-5 AC-7 redeployErrors validates the archive and the executable", () => {
    const form = { executable: "bin/server", archive: fileOfSize("nuevo.tar.gz", 10) };

    expect(redeployErrors(form)).toEqual({});
    expect(Object.keys(redeployErrors({ ...form, archive: null }))).toEqual(["archive"]);
    expect(Object.keys(redeployErrors({ ...form, executable: "../x" }))).toEqual(["executable"]);
    expect(Object.keys(redeployErrors({ executable: "a b", archive: fileOfSize("a.zip", 10) })).sort()).toEqual(["archive", "executable"]);
  });
});
