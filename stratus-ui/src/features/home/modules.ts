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
  {
    id: "web",
    title: "Servidores web",
    description: "Crear servidores Apache o Nginx, instalar su página estática y controlar el servicio.",
    path: "/web",
    enabled: true,
  },
  {
    id: "dns",
    title: "DNS",
    description: "Crear el servidor DNS, registrar los nombres de las VMs y usarlo como su resolvedor.",
    path: "/dns",
    enabled: true,
  },
];
