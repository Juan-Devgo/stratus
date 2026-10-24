import { Link } from "react-router";

import { Badge } from "@/components/ui/badge";
import { Card, CardAction, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

import type { MODULES } from "./modules";

type Props = { module: (typeof MODULES)[number] };

export function ModuleCard({ module }: Props) {
  const card = (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>{module.title}</CardTitle>
        <CardDescription>{module.description}</CardDescription>
        {!module.enabled && (
          <CardAction>
            <Badge variant="secondary">Próximamente</Badge>
          </CardAction>
        )}
      </CardHeader>
    </Card>
  );

  // Solo los módulos habilitados navegan; los demás se muestran sin enlace.
  if (!module.enabled) {
    return card;
  }
  return (
    <Link to={module.path} className="block h-full rounded-xl">
      {card}
    </Link>
  );
}
