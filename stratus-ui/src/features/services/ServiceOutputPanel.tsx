import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

type Props = { title: string; output: string };

// Tarjeta "Salida" con el texto de Estado o de Logs tal cual, en un bloque que conserva los saltos
// de línea (REQ-19, REQ-20). El texto es de solo lectura y no se interpreta como HTML.
export function ServiceOutputPanel({ title, output }: Props) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>Salida</CardDescription>
      </CardHeader>
      <CardContent>
        <pre className="max-h-96 overflow-auto rounded-lg bg-muted p-3 text-xs whitespace-pre-wrap">
          {output || "(sin salida)"}
        </pre>
      </CardContent>
    </Card>
  );
}
