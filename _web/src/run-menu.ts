export interface RunMenu {
  runButton: HTMLButtonElement;
  runAllButton: HTMLButtonElement;
  setRunning(value: boolean): void;
  setStatementCount(value: number): void;
  close(): void;
  destroy(): void;
}

/** Split execution control. Selection/current-statement semantics belong to the caller. */
export function createRunMenu(parent: HTMLElement, onRun: () => void, onRunAll: () => void, shortcut: string): RunMenu {
  parent.classList.add("run-controls");
  parent.innerHTML = `<div class="run-split"><button class="run run-primary" data-role="run" aria-label="Run current query"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 3l11 7-11 7z" fill="currentColor"/></svg></button><button class="run run-chevron" data-role="run-menu-toggle" aria-label="Execution options" aria-haspopup="menu" aria-expanded="false"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg></button></div><div class="run-menu" role="menu" aria-label="Execution options" hidden><button data-role="run-all" role="menuitem"><span>Run all</span><kbd></kbd></button></div>`;
  const runButton = parent.querySelector<HTMLButtonElement>('[data-role="run"]')!;
  const runAllButton = parent.querySelector<HTMLButtonElement>('[data-role="run-all"]')!;
  const toggle = parent.querySelector<HTMLButtonElement>('[data-role="run-menu-toggle"]')!;
  const menu = parent.querySelector<HTMLElement>('[role="menu"]')!;
  runButton.title = `Run selected text or current query (${shortcut})`;
  runAllButton.querySelector("kbd")!.textContent = shortcut.includes("⌘") ? "⌘ + Shift + Return" : "Ctrl + Shift + Enter";
  let running = false;
  let count = 0;
  const items = [runAllButton];
  function close(): void { menu.hidden = true; toggle.setAttribute("aria-expanded", "false"); }
  function open(last = false): void {
    if (toggle.disabled) return;
    menu.hidden = false;
    toggle.setAttribute("aria-expanded", "true");
    const enabled = items.filter((item) => !item.disabled);
    (last ? enabled.at(-1) : enabled[0])?.focus();
  }
  function paint(): void {
    runButton.disabled = runAllButton.disabled = toggle.disabled = running || count === 0;
    if (running || count === 0) close();
  }
  runButton.addEventListener("click", () => { close(); onRun(); });
  runAllButton.addEventListener("click", () => { close(); onRunAll(); });
  toggle.addEventListener("click", () => { if (menu.hidden) open(); else close(); });
  toggle.addEventListener("keydown", (event) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault(); open(event.key === "ArrowUp");
    }
  });
  parent.addEventListener("keydown", (event) => {
    if (menu.hidden) return;
    if (event.key === "Escape") { event.preventDefault(); close(); toggle.focus(); }
    if (event.key === "Tab") close();
    const enabled = items.filter((item) => !item.disabled);
    if (menu.contains(event.target as Node) && ["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      event.preventDefault();
      const index = enabled.indexOf(document.activeElement as HTMLButtonElement);
      const next = event.key === "Home" ? 0 : event.key === "End" ? enabled.length - 1 : (index + (event.key === "ArrowDown" ? 1 : -1) + enabled.length) % enabled.length;
      enabled[next]?.focus();
    }
  });
  const outside = (event: MouseEvent): void => { if (!parent.contains(event.target as Node)) close(); };
  document.addEventListener("click", outside);
  paint();
  return { runButton, runAllButton, close,
    setRunning(value) { running = value; paint(); },
    setStatementCount(value) { count = value; paint(); },
    destroy() { document.removeEventListener("click", outside); parent.replaceChildren(); },
  };
}
