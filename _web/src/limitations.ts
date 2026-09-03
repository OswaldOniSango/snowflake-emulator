import { LIMITATIONS } from "./limitations.generated";

/**
 * What the emulator does not do.
 *
 * The list is generated from the README, so this panel and the documentation
 * cannot disagree. It is a dialog rather than a page because it is something
 * you check mid-thought and dismiss.
 */
export function createLimitationsButton(): HTMLElement {
  const wrapper = document.createElement("span");

  const dialog = document.createElement("dialog");
  dialog.className = "limitations";

  const heading = document.createElement("h2");
  heading.textContent = "What this emulator does not do";

  const blurb = document.createElement("p");
  blurb.textContent =
    "A dev and test emulator, not a Snowflake replacement. These are the documented gaps.";

  const list = document.createElement("ul");
  for (const limitation of LIMITATIONS) {
    const item = document.createElement("li");
    item.textContent = limitation;
    list.append(item);
  }

  const close = document.createElement("button");
  close.className = "ghost";
  close.type = "button";
  close.textContent = "Close";
  close.addEventListener("click", () => dialog.close());

  dialog.append(heading, blurb, list, close);

  // Clicking the backdrop closes it, which is what the gesture means.
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) {
      dialog.close();
    }
  });

  const open = document.createElement("button");
  open.className = "footer-link";
  open.type = "button";
  open.textContent = `Limitations (${LIMITATIONS.length})`;
  open.addEventListener("click", () => dialog.showModal());

  wrapper.append(open, dialog);
  return wrapper;
}
