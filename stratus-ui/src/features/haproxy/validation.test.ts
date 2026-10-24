import { validateFrontendPort, validatePort } from "./validation";

const PORT_MESSAGE = "El puerto debe ser un número entre 1 y 65535";
const SSH_MESSAGE = "El puerto 22 está reservado para SSH";

describe("validatePort", () => {
  it("AC-16 rejects 0, 65536 and non numeric text", () => {
    for (const text of ["0", "65536", "80a", "", " ", "-1", "1.5", "08", "1e3", "100000"]) {
      expect(validatePort(text), text).toBe(PORT_MESSAGE);
    }
  });

  it("AC-16 accepts 1 to 65535, including 22", () => {
    for (const text of ["1", "22", "80", "8080", "65535"]) {
      expect(validatePort(text), text).toBeNull();
    }
  });
});

describe("validateFrontendPort", () => {
  it("AC-11 rejects 22, 0 and 65536", () => {
    expect(validateFrontendPort("22")).toBe(SSH_MESSAGE);
    expect(validateFrontendPort("0")).toBe(PORT_MESSAGE);
    expect(validateFrontendPort("65536")).toBe(PORT_MESSAGE);
    expect(validateFrontendPort("80a")).toBe(PORT_MESSAGE);
    expect(validateFrontendPort("")).toBe(PORT_MESSAGE);
  });

  it("AC-11 accepts 8080 and the other ports", () => {
    for (const text of ["8080", "80", "21", "23", "65535", "1"]) {
      expect(validateFrontendPort(text), text).toBeNull();
    }
  });
});
