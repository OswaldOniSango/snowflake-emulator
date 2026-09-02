import "./style.css";
import { checkHealth } from "./health";

// Static, author-controlled markup. Everything derived from data is built with
// createElement and textContent instead — later views render database, schema
// and table names that come from the emulator, and those must never be parsed
// as HTML.
const MARK = `
<svg viewBox="0 0 24 24" aria-hidden="true">
  <defs>
    <linearGradient id="mallard-mark" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#12866F" />
      <stop offset="1" stop-color="#0B4E52" />
    </linearGradient>
  </defs>
  <rect width="24" height="24" rx="7" fill="url(#mallard-mark)" />
  <circle cx="10.2" cy="9.9" r="4.3" fill="#FCFEFD" />
  <path d="M13.3 10.9 L20.2 12.1 L13.3 14.2 Z" fill="#E0A03E" />
  <circle cx="11.7" cy="9" r=".95" fill="#0B4E52" />
</svg>`;

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  node.className = className;
  if (text !== undefined) {
    node.textContent = text;
  }
  return node;
}

interface Shell {
  status: HTMLElement;
  label: HTMLElement;
}

function render(root: HTMLElement): Shell {
  const shell = element("main", "shell");

  const brand = element("div", "brand");
  const mark = element("span", "mark");
  mark.innerHTML = MARK;
  brand.append(mark, element("b", "", "Mallard"), element("span", "", "local"));

  const lede = element(
    "p",
    "lede",
    "Web console for the Snowflake emulator. The scaffolding is in place — the SQL worksheet lands next.",
  );

  const status = element("div", "status");
  status.dataset["state"] = "pending";
  status.setAttribute("role", "status");
  const label = element("span", "", "Checking emulator…");
  status.append(element("span", "dot"), label);

  const note = element("p", "note", "Served from the emulator binary at /.");

  shell.append(brand, lede, status, note);
  root.replaceChildren(shell);

  return { status, label };
}

async function main(): Promise<void> {
  const root = document.querySelector<HTMLElement>("#app");
  if (!root) {
    throw new Error("#app container missing from index.html");
  }

  const { status, label } = render(root);
  const health = await checkHealth();

  status.dataset["state"] = health.status;
  label.textContent =
    health.status === "ok"
      ? "Emulator healthy"
      : `Emulator unreachable — ${health.detail}`;
}

void main();
