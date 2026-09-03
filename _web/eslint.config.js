import js from "@eslint/js";
import tseslint from "typescript-eslint";

// Files that run under Node rather than in the browser: the build script and
// Vite's own config. Declared explicitly so no-undef stays on everywhere else.
const nodeGlobals = {
  console: "readonly",
  process: "readonly",
};

export default tseslint.config(
  // Vite's build output; publish-dist.mjs copies it into server/ui/dist.
  { ignores: ["dist/**"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["scripts/**/*.mjs", "vite.config.ts"],
    languageOptions: { globals: nodeGlobals },
  },
);
