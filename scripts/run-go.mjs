import { existsSync } from "node:fs";
import path from "node:path";
import process from "node:process";
import {
  attachExitHandlers,
  createGoEnv,
  rootDir,
  resolveGoCommand,
  spawnManaged
} from "./tooling.mjs";

const distIndex = path.join(rootDir, "dist", "index.html");
if (!existsSync(distIndex)) {
  console.warn("dist/index.html is missing. Run `npm run build` or `npm run start:build` if you need the frontend bundle.");
}

const goCommand = resolveGoCommand();
const server = spawnManaged(goCommand, ["run", "."], {
  env: createGoEnv({
    PORT: process.env.PORT ?? "8080"
  })
});

const shutdown = attachExitHandlers([server]);

server.on("exit", (code, signal) => {
  if (signal) {
    shutdown(1);
    return;
  }

  shutdown(code ?? 0);
});
