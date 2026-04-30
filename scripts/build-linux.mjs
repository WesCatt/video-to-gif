import { spawn } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import process from "node:process";
import { createGoEnv, resolveGoCommand, rootDir } from "./tooling.mjs";

const bundleDir = path.join(rootDir, "release", "video-to-gif-linux-amd64");
const distDir = path.join(rootDir, "dist");

if (!existsSync(path.join(distDir, "index.html"))) {
  throw new Error("dist/ is missing. Run `npm run build` before `npm run build:linux`.");
}

rmIfExists(bundleDir);
mkdirSync(bundleDir, { recursive: true });
mkdirSync(path.join(bundleDir, "outputs"), { recursive: true });
mkdirSync(path.join(bundleDir, "temp"), { recursive: true });

await copyDirectory(path.join(rootDir, "dist"), path.join(bundleDir, "dist"));
cpIfExists(path.join(rootDir, ".env.example"), path.join(bundleDir, ".env.example"));

await run(resolveGoCommand(), ["build", "-o", path.join(bundleDir, "video-to-gif"), "."], {
  env: createGoEnv({
    GOOS: "linux",
    GOARCH: "amd64",
    CGO_ENABLED: "0"
  })
});

writeFileSync(path.join(bundleDir, "start.sh"), `#!/usr/bin/env sh
set -eu

PORT="\${PORT:-430}"
export PORT

exec ./video-to-gif
`);

writeFileSync(path.join(bundleDir, "DEPLOY.md"), `# Linux Deployment

## Included files

- \`video-to-gif\`: Linux amd64 backend binary
- \`dist/\`: frontend static assets
- \`outputs/\`: local GIF output or OpenList metadata directory
- \`temp/\`: temporary working directory
- \`.env.example\`: environment variable example
- \`start.sh\`: startup script, default port is \`430\`

## Requirements on the server

- Linux amd64
- \`ffmpeg\` available in \`PATH\`

## Start

\`\`\`bash
chmod +x video-to-gif start.sh
./start.sh
\`\`\`

Or specify the port explicitly:

\`\`\`bash
PORT=430 ./start.sh
\`\`\`

## OpenList mode

If you want to store GIFs in OpenList, create a \`.env\` file next to the binary:

\`\`\`env
OPENLIST_BASE_URL=https://your-openlist.example.com
OPENLIST_USERNAME=your-openlist-username
OPENLIST_PASSWORD=your-openlist-password
OPENLIST_VIDEO_PATH=/video-to-gif
\`\`\`
`);

console.log(`Linux bundle created at: ${bundleDir}`);

function rmIfExists(targetPath) {
  if (!existsSync(targetPath)) {
    return;
  }

  rmSync(targetPath, { recursive: true, force: true });
}

function cpIfExists(source, destination) {
  if (existsSync(source)) {
    copyFileSync(source, destination);
  }
}

async function copyDirectory(source, destination) {
  if (process.platform === "win32") {
    const escapedSource = source.replace(/'/g, "''");
    const escapedDestination = destination.replace(/'/g, "''");
    await run("powershell.exe", [
      "-NoProfile",
      "-Command",
      `New-Item -ItemType Directory -Force -Path '${escapedDestination}' | Out-Null; Copy-Item -Path '${escapedSource}\\*' -Destination '${escapedDestination}' -Recurse -Force`
    ]);
    return;
  }

  const { cpSync } = await import("node:fs");
  cpSync(source, destination, { recursive: true, force: true });
}

function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: rootDir,
      stdio: "inherit",
      shell: false,
      ...options
    });

    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }

      reject(new Error(`${command} ${args.join(" ")} exited with code ${code ?? 1}`));
    });

    child.on("error", reject);
  });
}
