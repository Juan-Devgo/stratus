import { createUserErrors, validatePassword, validatePasswordConfirmation, validateUsername } from "./validation";

describe("validation", () => {
  it("AC-5 rejects invalid usernames", () => {
    for (const name of ["Ana", "1ana", "ana;rm", "", "a".repeat(33), "ana b", "-ana"]) {
      expect(validateUsername(name), JSON.stringify(name)).toEqual(expect.any(String));
    }
    for (const name of ["ana", "ana_1-b", "a".repeat(32)]) {
      expect(validateUsername(name), name).toBeNull();
    }
  });

  it("AC-7 rejects invalid passwords", () => {
    for (const password of ["Corta12", "x".repeat(73), "Contraseña1", " Secreta123", "Secreta123 ", ""]) {
      expect(validatePassword(password), JSON.stringify(password)).toEqual(expect.any(String));
    }
    for (const password of ["Secreta1", "x".repeat(72), "una clave con espacios", "!~Secreta123~!"]) {
      expect(validatePassword(password), password).toBeNull();
    }
    expect(validatePasswordConfirmation("Secreta123", "Secreta124")).toEqual(expect.any(String));
    expect(validatePasswordConfirmation("Secreta123", "Secreta123")).toBeNull();
  });

  it("AC-5 AC-7 AC-8 createUserErrors reports each field and ignores passwords with keys", () => {
    const base = { username: "ana", sudo: false, ssh_key: false, password: "Secreta123", confirmation: "Secreta123" };

    expect(createUserErrors(base)).toEqual({});
    expect(Object.keys(createUserErrors({ ...base, username: "Ana" }))).toEqual(["username"]);
    expect(Object.keys(createUserErrors({ ...base, password: "Corta12", confirmation: "Corta12" }))).toEqual(["password"]);
    expect(Object.keys(createUserErrors({ ...base, confirmation: "otra" }))).toEqual(["confirmation"]);
    // Con llaves los campos de contraseña no existen: no se validan aunque tengan texto.
    expect(createUserErrors({ ...base, ssh_key: true, password: "x", confirmation: "y" })).toEqual({});
  });
});
