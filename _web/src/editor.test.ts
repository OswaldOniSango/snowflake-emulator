import { afterEach, describe, expect, it } from "vitest";
import { createEditor } from "./editor";

function host(): HTMLElement {
  const element = document.createElement("div");
  document.body.append(element);
  return element;
}

afterEach(() => {
  document.body.replaceChildren();
});

describe("highlightRunning", () => {
  it("marks the given range's line and clears it when passed null", () => {
    const parent = host();
    const editor = createEditor({ parent, initialValue: "SELECT 1;\nSELECT 2;", onRun: () => {} });

    expect(parent.querySelectorAll(".cm-runningStatement")).toHaveLength(0);

    editor.highlightRunning({ from: 0, to: 8 }); // "SELECT 1"
    expect(parent.querySelectorAll(".cm-runningStatement")).toHaveLength(1);

    editor.highlightRunning(null);
    expect(parent.querySelectorAll(".cm-runningStatement")).toHaveLength(0);
  });

  it("marks every line a multi-line statement spans", () => {
    const parent = host();
    const editor = createEditor({ parent, initialValue: "SELECT\n  1,\n  2;", onRun: () => {} });

    editor.highlightRunning({ from: 0, to: 15 }); // the whole buffer, three lines
    expect(parent.querySelectorAll(".cm-runningStatement")).toHaveLength(3);
  });

  it("moves the highlight to the next statement instead of stacking it", () => {
    const parent = host();
    const editor = createEditor({ parent, initialValue: "SELECT 1;\nSELECT 2;", onRun: () => {} });

    editor.highlightRunning({ from: 0, to: 8 });
    editor.highlightRunning({ from: 10, to: 18 });

    expect(parent.querySelectorAll(".cm-runningStatement")).toHaveLength(1);
  });
});

describe("selectionRange", () => {
  it("is null when nothing is selected", () => {
    const editor = createEditor({ parent: host(), initialValue: "SELECT 1;", onRun: () => {} });
    expect(editor.selectionRange()).toBeNull();
  });
});
