import process from "node:process";
import {
  attachExitHandlers,
  createGoEnv,
  resolveGoCommand,
  resolveViteEntrypoint,
  spawnManaged
} from "./tooling.mjs";

const server = spawnManaged(resolveGoCommand(), ["run", "."], {
  env: createGoEnv({
    PORT: process.env.PORT ?? "8080"
  })
});

const frontend = spawnManaged(process.execPath, [resolveViteEntrypoint()], {
  env: {
    ...process.env
  }
});

const shutdown = attachExitHandlers([server, frontend]);

server.on("exit", (code) => shutdown(code ?? 0));
frontend.on("exit", (code) => shutdown(code ?? 0));
