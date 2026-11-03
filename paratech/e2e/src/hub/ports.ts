import net from "node:net";

import { BadConfigError, ForbiddenPortError } from "./errors.js";

export const Loopback = "127.0.0.1";

// Ports of the owner's Hub (local run and the compose container): a test Hub never listens there.
export const ForbiddenHubPorts: readonly number[] = [8080, 58080];

export function assertAllowedHubPort(port: number): void {
  if (ForbiddenHubPorts.includes(port)) {
    throw new ForbiddenPortError(port);
  }
}

/** Parses a port number from a variable; the message names the variable, never other values. */
export function parsePort(raw: string, name: string): number {
  const n = Number(raw);
  if (!/^\d+$/.test(raw) || !Number.isInteger(n) || n < 1 || n > 65535) {
    throw new BadConfigError(`${name}: нужен номер порта 1..65535`);
  }
  return n;
}

/** A port the OS just gave to a loopback listener; it is free unless someone takes it before the caller binds. */
export async function freeLoopbackPort(): Promise<number> {
  for (let attempt = 0; attempt < 10; attempt++) {
    const port = await new Promise<number>((resolve, reject) => {
      const srv = net.createServer();
      srv.once("error", reject);
      srv.listen({ host: Loopback, port: 0, exclusive: true }, () => {
        const addr = srv.address();
        srv.close(() => resolve(typeof addr === "object" && addr ? addr.port : 0));
      });
    });
    if (port > 0 && !ForbiddenHubPorts.includes(port)) {
      return port;
    }
  }
  throw new BadConfigError("Не удалось получить свободный порт на 127.0.0.1");
}

/**
 * Whether a loopback port is taken: something accepts connections on it, or it cannot be bound (in use, or reserved
 * by Windows for Hyper-V/WSL). Both checks, because on Windows a wildcard listener does not always block a loopback bind.
 */
export async function loopbackPortBusy(port: number): Promise<boolean> {
  const accepts = await new Promise<boolean>((resolve) => {
    const sock = net.connect({ host: Loopback, port });
    sock.setTimeout(1000);
    sock.once("connect", () => {
      sock.destroy();
      resolve(true);
    });
    sock.once("timeout", () => {
      sock.destroy();
      resolve(false);
    });
    sock.once("error", () => resolve(false));
  });
  if (accepts) {
    return true;
  }
  return new Promise<boolean>((resolve) => {
    const srv = net.createServer();
    srv.once("error", () => resolve(true));
    srv.listen({ host: Loopback, port, exclusive: true }, () => srv.close(() => resolve(false)));
  });
}
