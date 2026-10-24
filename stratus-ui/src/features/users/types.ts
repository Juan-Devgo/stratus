// Tipos de la API del módulo de usuarios (campos JSON en snake_case, como los envía el backend).

// Un usuario regular de la VM (UID 1000-59999).
export interface UserRow {
  username: string;
  uid: number;
  sudo: boolean;
  ssh_key: boolean;
}

// Cuerpo de POST /api/users/vms/{vm}/users; password se omite cuando ssh_key es true.
export interface CreateUserInput {
  username: string;
  sudo: boolean;
  ssh_key: boolean;
  password?: string;
}
