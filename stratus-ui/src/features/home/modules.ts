// Lista única de módulos: de aquí salen las tarjetas del inicio y las rutas futuras.
export const MODULES: readonly {
  id: string;
  title: string;
  description: string;
  path: string;
  enabled: boolean;
}[] = [
  {
    id: "prepare",
    title: "Preparar sistema",
    description: "Crear máquinas virtuales Debian 13 e instalar paquetes, llaves SSH, hostname y /etc/issue.",
    path: "/prepare",
    enabled: true,
  },
  {
    id: "users",
    title: "Gestión de usuarios",
    description: "Crear y eliminar usuarios de la VM, con sudo y llaves SSH opcionales.",
    path: "/users",
    enabled: true,
  },
  {
    id: "services",
    title: "Servicios systemd",
    description: "Desplegar aplicaciones como servicios systemd y controlarlos desde la consola.",
    path: "/services",
    enabled: true,
  },
  {
    id: "haproxy",
    title: "Backends HAProxy",
    description: "Crear balanceadores HAProxy, asignar servidores y ver su estado.",
    path: "/haproxy",
    enabled: true,
  },
];
