import { Route, Routes } from "react-router";

import { AppLayout } from "./components/AppLayout";
import { HAProxy } from "./routes/HAProxy";
import { Home } from "./routes/Home";
import { Prepare } from "./routes/Prepare";
import { Services } from "./routes/Services";
import { Users } from "./routes/Users";

export function App() {
  return (
    <Routes>
      {/* Ruta de diseño: cada módulo futuro agrega aquí un <Route> hijo. */}
      <Route element={<AppLayout />}>
        <Route path="/" element={<Home />} />
        <Route path="/prepare" element={<Prepare />} />
        <Route path="/users" element={<Users />} />
        <Route path="/services" element={<Services />} />
        <Route path="/haproxy" element={<HAProxy />} />
      </Route>
    </Routes>
  );
}
