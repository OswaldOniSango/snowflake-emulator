import { describe, expect, it, vi } from "vitest";
import { cancelStatement, CODE_CANCELED, runStatement, StatementError } from "./api";

const context = { database: "TEST_DB", schema: "PUBLIC" };

/** A fetch that answers each call with the next body it was given. */
function fetchReturning(...bodies: unknown[]): typeof fetch {
  const queue = [...bodies];
  return vi.fn(async () =>
    new Response(JSON.stringify(queue.length > 1 ? queue.shift() : queue[0]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  ) as unknown as typeof fetch;
}

const pending = { statementHandle: "01abc", code: "333334", sqlState: "00000" };
const succeeded = {
  statementHandle: "01abc",
  code: "090001",
  sqlState: "00000",
  resultSetMetaData: { numRows: 1, format: "jsonv2", rowType: [{ name: "N", type: "FIXED" }] },
  data: [[1]],
};

describe("runStatement", () => {
  it("submits asynchronously so the statement has a handle to cancel", async () => {
    const fetchFn = fetchReturning(succeeded);
    await runStatement("SELECT 1", context, fetchFn);

    const [, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(JSON.parse((init as RequestInit).body as string)).toMatchObject({ async: true });
  });

  it("reports the handle as soon as the statement is accepted", async () => {
    const onHandle = vi.fn();
    await runStatement("SELECT 1", context, fetchReturning(succeeded), onHandle);
    expect(onHandle).toHaveBeenCalledWith("01abc");
  });

  it("reports the handle before the result arrives, so Cancel can be offered", async () => {
    const seen: string[] = [];
    const fetchFn = fetchReturning(pending, pending, succeeded);
    await runStatement("SELECT 1", context, fetchFn, (handle) => seen.push(handle));

    // One call, made on acceptance rather than on completion.
    expect(seen).toEqual(["01abc"]);
  });

  it("polls until the statement stops reporting itself as pending", async () => {
    const fetchFn = fetchReturning(pending, pending, succeeded);
    const result = await runStatement("SELECT 1", context, fetchFn);

    expect(result.rows).toEqual([[1]]);
    // Submit, then two status requests.
    expect(fetchFn).toHaveBeenCalledTimes(3);
  });

  it("polls the statement's own status URL", async () => {
    const fetchFn = fetchReturning(pending, succeeded);
    await runStatement("SELECT 1", context, fetchFn);

    const [url] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[1]!;
    expect(url).toBe("/api/v2/statements/01abc");
  });

  it("raises a cancellation as such, not as an empty result", async () => {
    const canceled = {
      statementHandle: "01abc",
      code: CODE_CANCELED,
      // The emulator reports the success SQL state for a cancellation, which
      // is why the code has to be what decides.
      sqlState: "00000",
      message: "Statement canceled",
    };

    await expect(runStatement("SELECT 1", context, fetchReturning(pending, canceled))).rejects.toThrow(
      StatementError,
    );
    await runStatement("SELECT 1", context, fetchReturning(pending, canceled)).catch((error: unknown) => {
      expect((error as StatementError).code).toBe(CODE_CANCELED);
    });
  });

  it("still raises an ordinary failure", async () => {
    const failed = {
      statementHandle: "01abc",
      code: "002003",
      sqlState: "42000",
      message: "Object 'NOPE' does not exist",
    };

    await expect(runStatement("SELECT 1", context, fetchReturning(failed))).rejects.toThrow(
      "Object 'NOPE' does not exist",
    );
  });
});

describe("cancelStatement", () => {
  it("posts to the statement's cancel endpoint", async () => {
    const fetchFn = vi.fn(async () => new Response("{}", { status: 200 })) as unknown as typeof fetch;
    await cancelStatement("01abc", fetchFn);

    expect(fetchFn).toHaveBeenCalledWith("/api/v2/statements/01abc/cancel", { method: "POST" });
  });

  it("escapes a handle before putting it in the path", async () => {
    const fetchFn = vi.fn(async () => new Response("{}", { status: 200 })) as unknown as typeof fetch;
    await cancelStatement("a/b", fetchFn);

    expect(fetchFn).toHaveBeenCalledWith("/api/v2/statements/a%2Fb/cancel", { method: "POST" });
  });

  it("does not raise when the statement had already finished", async () => {
    const fetchFn = vi.fn(async () =>
      new Response('{"message":"not running"}', { status: 400 }),
    ) as unknown as typeof fetch;

    await expect(cancelStatement("01abc", fetchFn)).resolves.toBeUndefined();
  });
});
