import { describe, expect, it } from "vitest";
import {
  DEFAULT_CONTEXT,
  loadWorkspace,
  nextWorksheetName,
  newWorksheet,
  saveWorkspace,
  type Workspace,
  reorderWorksheets,
  type Worksheet,
} from "./workspace";

/** A stand-in for localStorage that can also be told to fail. */
function fakeStorage(initial: Record<string, string> = {}, failing = false): Storage {
  const data = new Map(Object.entries(initial));
  const boom = () => {
    throw new Error("storage unavailable");
  };
  return {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    key: (index: number) => [...data.keys()][index] ?? null,
    getItem: (key: string) => (failing ? boom() : (data.get(key) ?? null)),
    setItem: (key: string, value: string) => {
      if (failing) {
        boom();
      }
      data.set(key, value);
    },
    removeItem: (key: string) => void data.delete(key),
  } as Storage;
}

const stored = (workspace: Workspace, version = 1): Record<string, string> => ({
  "mallard.workspace": JSON.stringify({ version, ...workspace }),
});

describe("loadWorkspace", () => {
  it("starts with one worksheet when nothing is stored", () => {
    const workspace = loadWorkspace(fakeStorage());

    expect(workspace.worksheets).toHaveLength(1);
    expect(workspace.activeId).toBe(workspace.worksheets[0]?.id);
    expect(workspace.worksheets[0]?.context).toEqual(DEFAULT_CONTEXT);
  });

  it("restores what was saved", () => {
    const worksheet = newWorksheet("Analysis", { database: "OTHER", schema: "STAGING" }, "SELECT 1");
    const workspace = { worksheets: [worksheet], activeId: worksheet.id };

    expect(loadWorkspace(fakeStorage(stored(workspace)))).toEqual(workspace);
  });

  it("starts fresh when storage is missing entirely", () => {
    expect(loadWorkspace(null).worksheets).toHaveLength(1);
  });

  it("starts fresh when reading throws", () => {
    // A private window can refuse outright rather than return null.
    expect(loadWorkspace(fakeStorage({}, true)).worksheets).toHaveLength(1);
  });

  it("starts fresh when the stored value is not JSON", () => {
    expect(loadWorkspace(fakeStorage({ "mallard.workspace": "{ not json" })).worksheets).toHaveLength(1);
  });

  it("discards a workspace written by an older version", () => {
    const worksheet = newWorksheet("Old");
    const old = stored({ worksheets: [worksheet], activeId: worksheet.id }, 0);

    const workspace = loadWorkspace(fakeStorage(old));
    expect(workspace.worksheets[0]?.name).not.toBe("Old");
  });

  it("drops entries that are not worksheets", () => {
    const good = newWorksheet("Keep");
    const raw = {
      "mallard.workspace": JSON.stringify({
        version: 1,
        worksheets: [good, null, 42, { name: "no id" }],
        activeId: good.id,
      }),
    };

    const workspace = loadWorkspace(fakeStorage(raw));
    expect(workspace.worksheets).toHaveLength(1);
    expect(workspace.worksheets[0]?.name).toBe("Keep");
  });

  it("fills in a worksheet missing its sql or context", () => {
    const raw = {
      "mallard.workspace": JSON.stringify({
        version: 1,
        worksheets: [{ id: "w1", name: "Partial" }],
        activeId: "w1",
      }),
    };

    const worksheet = loadWorkspace(fakeStorage(raw)).worksheets[0];
    expect(worksheet?.sql).toBe("");
    expect(worksheet?.context).toEqual(DEFAULT_CONTEXT);
  });

  it("falls back to the first worksheet when the active id is unknown", () => {
    const worksheet = newWorksheet("Only");
    const raw = stored({ worksheets: [worksheet], activeId: "gone" });

    expect(loadWorkspace(fakeStorage(raw)).activeId).toBe(worksheet.id);
  });

  it("starts fresh when every stored worksheet was dropped", () => {
    const raw = {
      "mallard.workspace": JSON.stringify({ version: 1, worksheets: [null], activeId: "x" }),
    };

    expect(loadWorkspace(fakeStorage(raw)).worksheets).toHaveLength(1);
  });
});

describe("saveWorkspace", () => {
  it("round-trips through storage", () => {
    const storage = fakeStorage();
    const worksheet = newWorksheet("Saved", DEFAULT_CONTEXT, "SELECT 1");
    const workspace = { worksheets: [worksheet], activeId: worksheet.id };

    saveWorkspace(workspace, storage);

    expect(loadWorkspace(storage)).toEqual(workspace);
  });

  it("says nothing when storage refuses", () => {
    // Losing the buffer is bad; taking the console down with it is worse.
    expect(() => saveWorkspace(defaultish(), fakeStorage({}, true))).not.toThrow();
    expect(() => saveWorkspace(defaultish(), null)).not.toThrow();
  });
});

describe("nextWorksheetName", () => {
  it("counts up from the number open", () => {
    expect(nextWorksheetName([])).toBe("Worksheet 1");
    expect(nextWorksheetName([newWorksheet("Worksheet 1")])).toBe("Worksheet 2");
  });

  it("skips a name already taken", () => {
    const open = [newWorksheet("Worksheet 1"), newWorksheet("Worksheet 3")];
    // Two are open, so it tries 3, which is taken, and lands on 4.
    expect(nextWorksheetName(open)).toBe("Worksheet 4");
  });
});

function defaultish(): Workspace {
  const worksheet = newWorksheet("W");
  return { worksheets: [worksheet], activeId: worksheet.id };
}

describe("reorderWorksheets", () => {
  const sheets = (...names: string[]) => names.map((name) => newWorksheet(name));
  const names = (list: Worksheet[]) => list.map((worksheet) => worksheet.name);

  it("drops a tab before another", () => {
    const list = sheets("A", "B", "C");
    const moved = reorderWorksheets(list, list[2]!.id, list[0]!.id, "before");
    expect(names(moved)).toEqual(["C", "A", "B"]);
  });

  it("drops a tab after another", () => {
    const list = sheets("A", "B", "C");
    const moved = reorderWorksheets(list, list[0]!.id, list[1]!.id, "after");
    expect(names(moved)).toEqual(["B", "A", "C"]);
  });

  it("can move a tab to the very end", () => {
    const list = sheets("A", "B", "C");
    const moved = reorderWorksheets(list, list[0]!.id, list[2]!.id, "after");
    expect(names(moved)).toEqual(["B", "C", "A"]);
  });

  it("can move a tab to the very front", () => {
    const list = sheets("A", "B", "C");
    const moved = reorderWorksheets(list, list[2]!.id, list[0]!.id, "before");
    expect(names(moved)).toEqual(["C", "A", "B"]);
  });

  it("moving a neighbour one place back is not a no-op", () => {
    // Removing before inserting is what makes this land correctly; computing
    // the index up front would leave B where it started.
    const list = sheets("A", "B", "C");
    const moved = reorderWorksheets(list, list[1]!.id, list[2]!.id, "after");
    expect(names(moved)).toEqual(["A", "C", "B"]);
  });

  it("returns the same list when a tab is dropped on itself", () => {
    const list = sheets("A", "B");
    expect(reorderWorksheets(list, list[0]!.id, list[0]!.id, "before")).toBe(list);
  });

  it("returns the same list for an id the workspace does not hold", () => {
    const list = sheets("A", "B");
    expect(reorderWorksheets(list, "missing", list[0]!.id, "before")).toBe(list);
    expect(reorderWorksheets(list, list[0]!.id, "missing", "before")).toBe(list);
  });

  it("leaves the original list untouched", () => {
    const list = sheets("A", "B", "C");
    reorderWorksheets(list, list[0]!.id, list[2]!.id, "after");
    expect(names(list)).toEqual(["A", "B", "C"]);
  });
});
