// Turns the README's Limitations section into a module the console imports.
//
// The console has to tell people what the emulator does not do, and that list
// already exists in the README. Copying it into the frontend would guarantee
// the two drift apart, so it is read instead — the README stays the one place
// the list is written.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const readme = join(webRoot, "..", "README.md");
const target = join(webRoot, "src", "limitations.generated.ts");

const HEADING = "## Limitations";

const markdown = readFileSync(readme, "utf8");
const start = markdown.indexOf(HEADING);
if (start < 0) {
  console.error(`generate-limitations: no "${HEADING}" section in README.md`);
  process.exit(1);
}

const body = markdown.slice(start + HEADING.length);
const end = body.search(/^## /m);
const section = end < 0 ? body : body.slice(0, end);

const limitations = section
  .split("\n")
  .filter((line) => line.startsWith("- "))
  .map((line) => line.slice(2).trim())
  // Markdown code spans are the only formatting the list uses; the console
  // renders plain text, so the backticks come off.
  .map((line) => line.replaceAll("`", ""))
  .filter(Boolean);

if (limitations.length === 0) {
  console.error("generate-limitations: the Limitations section has no entries");
  process.exit(1);
}

const contents = `// Generated from README.md by scripts/generate-limitations.mjs.
// Do not edit: change the README's Limitations section instead.

/** What the emulator does not support, as documented in the README. */
export const LIMITATIONS: readonly string[] = [
${limitations.map((line) => `  ${JSON.stringify(line)},`).join("\n")}
];
`;

writeFileSync(target, contents);
console.log(`generate-limitations: wrote ${limitations.length} entries to ${target}`);
