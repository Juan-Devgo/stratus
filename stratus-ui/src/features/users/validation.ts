// Listas permitidas del módulo de usuarios (P3). Son las mismas reglas de
// stratus-srv/internal/service/users_validation.go y de create-user.sh; cada función devuelve el
// mensaje de error en español o null si el valor es válido.

const USERNAME_PATTERN = /^[a-z][a-z0-9_-]{0,31}$/;
// 8 a 72 caracteres ASCII imprimibles (0x20-0x7E), sin espacio al inicio ni al final.
const PASSWORD_PATTERN = /^[\x21-\x7E][\x20-\x7E]{6,70}[\x21-\x7E]$/;

export interface CreateUserForm {
  username: string;
  sudo: boolean;
  ssh_key: boolean;
  password: string;
  confirmation: string;
}

export type CreateUserFormErrors = Partial<Record<"username" | "password" | "confirmation", string>>;

export function validateUsername(username: string): string | null {
  if (!USERNAME_PATTERN.test(username)) {
    return 'El nombre de usuario debe tener de 1 a 32 caracteres: minúsculas, números, "_" y "-", empezando con una letra';
  }
  return null;
}

export function validatePassword(password: string): string | null {
  if (!PASSWORD_PATTERN.test(password)) {
    return "La contraseña debe tener de 8 a 72 caracteres ASCII imprimibles, sin espacio al inicio ni al final";
  }
  return null;
}

export function validatePasswordConfirmation(password: string, confirmation: string): string | null {
  return password === confirmation ? null : "La confirmación no coincide con la contraseña";
}

// Errores por campo, para mostrar cada uno bajo su control. Con llaves SSH el formulario no tiene
// campos de contraseña, así que no se validan (REQ-8).
export function createUserErrors(form: CreateUserForm): CreateUserFormErrors {
  const errors: CreateUserFormErrors = {};
  const username = validateUsername(form.username);
  if (username) {
    errors.username = username;
  }
  if (!form.ssh_key) {
    const password = validatePassword(form.password);
    if (password) {
      errors.password = password;
    } else {
      const confirmation = validatePasswordConfirmation(form.password, form.confirmation);
      if (confirmation) {
        errors.confirmation = confirmation;
      }
    }
  }
  return errors;
}
