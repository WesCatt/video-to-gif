import { spawn } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
export const rootDir = path.resolve(scriptDir, "..");
const isWin = process.platform === "win32";

function resolveLocalBinary(...segments) {
  const target = path.join(rootDir, ...segments);
  return existsSync(target) ? target : null;
}

export function createGoEnv(extraEnv = {}) {
  const env = {
    ...process.env,
    ...extraEnv
  };

  const localFFmpegBin = resolveLocalBinary(".tools", "ffmpeg", "bin");
  if (localFFmpegBin) {
    env.PATH = `${localFFmpegBin}${path.delimiter}${env.PATH ?? ""}`;
  }

  const goCacheDir = path.join(rootDir, ".tools", "go-cache");
  mkdirSync(goCacheDir, { recursive: true });
  env.GOCACHE ||= goCacheDir;

  return env;
}

export function resolveGoCommand() {
  return resolveLocalBinary(".tools", "go", "bin", isWin ? "go.exe" : "go") ?? "go";
}

export function resolveViteEntrypoint() {
  return path.join(rootDir, "node_modules", "vite", "bin", "vite.js");
}

export function spawnManaged(command, args, options = {}) {
  const child = spawn(command, args, {
    cwd: rootDir,
    stdio: "inherit",
    shell: false,
    ...options
  });

  child.on("error", (error) => {
    const detail = error.code === "ENOENT"
      ? `Command not found: ${command}`
      : error.message;
    console.error(detail);
  });

  return child;
}

export function terminateChild(child) {
  if (!child || child.exitCode !== null || child.killed) {
    return;
  }

  if (isWin) {
    spawn("taskkill", ["/pid", String(child.pid), "/t", "/f"], {
      stdio: "ignore",
      windowsHide: true
    });
    return;
  }

  child.kill("SIGTERM");
}

export function attachExitHandlers(children) {
  let shuttingDown = false;

  const shutdown = (code = 0) => {
    if (shuttingDown) {
      return;
    }

    shuttingDown = true;
    for (const child of children) {
      terminateChild(child);
    }

    setTimeout(() => {
      process.exit(code);
    }, 200);
  };

  process.on("SIGINT", () => shutdown(0));
  process.on("SIGTERM", () => shutdown(0));

  return shutdown;
}
