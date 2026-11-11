// Contenido de una celda de FQDN (REQ-45): "-" sin enlace si no hay FQDN; si lo hay, un enlace a
// http://<fqdn>/ que se abre en otra pestaña. Solo resuelve si el equipo del navegador usa el
// servidor DNS como resolvedor.
export function FqdnLink({ fqdn }: { fqdn: string }) {
  if (!fqdn) {
    return <>-</>;
  }
  return (
    <a
      href={`http://${fqdn}/`}
      target="_blank"
      rel="noopener noreferrer"
      className="text-primary underline-offset-4 hover:underline"
    >
      {fqdn}
    </a>
  );
}
