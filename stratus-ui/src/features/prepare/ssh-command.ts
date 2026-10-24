import type { Vm } from "./types";

// Comando ssh que se muestra al usuario para entrar a la VM con su llave. Un servidor asignado a un
// balanceador no tiene IP alcanzable desde la LAN: se entra con "ssh -J" por la cuenta jump del
// balanceador (REQ-18). Un balanceador y una VM común se alcanzan directo. Mientras falte una
// dirección se muestra un marcador entre < > para que el usuario sepa qué completar.
export function sshCommand(vm: Vm, keyFile: string, user: string): string {
  if (vm.role === "server") {
    return `ssh -J jump@${vm.lb_ip || "<IP del balanceador>"} -i ${keyFile} ${user}@${vm.ip || "<IP interna>"}`;
  }
  return `ssh -i ${keyFile} ${user}@${vm.ip || "<IP de la VM>"}`;
}
