// Publishes the Vite bundle into the Go package that embeds it.
//
// Vite owns _web/dist and is free to empty it. server/ui/dist is a Go package
// directory holding a tracked .gitkeep, which is what lets go:embed compile in
// a clone where nobody ran the frontend build. Copying rather than pointing
// Vite straight at it keeps a failed or interrupted build from deleting that
// placeholder and breaking `go build` for the whole repository.
import { cpSync, existsSync, mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = join(webRoot, "dist");
const target = resolve(webRoot, "..", "server", "ui", "dist");
const placeholder = ".gitkeep";

if (!existsSync(join(source, "index.html"))) {
  console.error(`publish-dist: ${source}/index.html is missing; did vite build run?`);
  process.exit(1);
}

mkdirSync(target, { recursive: true });
for (const entry of readdirSync(target)) {
  if (entry !== placeholder) {
    rmSync(join(target, entry), { recursive: true, force: true });
  }
}

cpSync(source, target, { recursive: true });
writeFileSync(join(target, placeholder), "");

console.log(`publish-dist: bundle published to ${target}`);
