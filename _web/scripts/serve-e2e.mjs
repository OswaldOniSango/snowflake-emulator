import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";

// Both stage files and the executable belong to this disposable test instance.
const directory = await mkdtemp(join(tmpdir(), "mallard-journey-"));
const root = fileURLToPath(new URL("../../", import.meta.url));
const binary = join(directory, process.platform === "win32" ? "server.exe" : "server");
let child;
let stopping = false;
const stop = () => {
  stopping = true;
  child?.kill("SIGTERM");
};
process.on("SIGTERM", stop);
process.on("SIGINT", stop);

function run(command, args, env = process.env) {
  return new Promise((resolve, reject) => {
    child = spawn(command, args, { cwd: root, env, stdio: "inherit" });
    child.once("error", reject);
    child.once("exit", (code) => resolve(code ?? (stopping ? 0 : 1)));
  });
}

try {
  const code = await run("go", ["build", "-o", binary, "./cmd/server"]);
  process.exitCode = code;
  if (code === 0 && !stopping) {
    process.exitCode = await run(binary, [], {
      ...process.env,
      DB_PATH: ":memory:",
      STAGE_DIR: join(directory, "stages"),
    });
  }
} finally {
  await rm(directory, { recursive: true, force: true });
}
