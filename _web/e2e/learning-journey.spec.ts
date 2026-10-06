import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";
import { splitStatements } from "../src/statements";

const journey = new URL("../../examples/learning-journey/", import.meta.url);
const sqlFile = (name: string): string => readFileSync(new URL(name, journey), "utf8");

async function login(page: Page, username: string, password: string): Promise<void> {
  await page.getByLabel("Username", { exact: true }).fill(username);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("button", { name: "Sign out", exact: true })).toBeVisible();
}

async function logout(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in to Mallard" })).toBeVisible();
}

async function execute(page: Page, sql: string, succeeds = true, runAll = false): Promise<void> {
  await page.locator(".cm-content").fill(sql);
  const lastStatement = runAll ? splitStatements(sql).at(-1)!.text : sql;
  // Wait for this exact submission so a previous Succeeded state cannot pass.
  const submitted = page.waitForResponse((response) => {
    const request = response.request();
    return request.method() === "POST" && response.url().endsWith("/api/v2/statements")
      && request.postDataJSON()?.statement === lastStatement.trim().replace(/;$/, "");
  }, { timeout: 15_000 });
  await page.locator(runAll ? '[data-role="run-all"]' : '[data-role="run"]').click();
  await submitted;
  await expect(page.locator('[data-role="pill"]')).toHaveText(succeeds ? "Succeeded" : "Failed");
  if (!succeeds) {
    await expect(page.locator('[data-role="dock"]')).toContainText(
      /insufficient privileges|role is not granted to user/,
    );
  }
}

async function chapter(page: Page, name: string, succeeds = true, check?: (sql: string) => Promise<void>): Promise<void> {
  for (const statement of splitStatements(sqlFile(name))) {
    await test.step(statement.text.slice(0, 100), async () => {
      await execute(page, statement.text, succeeds);
      await check?.(statement.text);
    });
  }
}

async function chooseNamespace(page: Page, database: string): Promise<void> {
  await page.getByRole("button", { name: "Choose database and schema" }).click();
  const namespaces = page.locator(".namespace-popover");
  await namespaces.getByRole("button", { name: database, exact: false }).click();
  await namespaces.getByRole("button", { name: "PUBLIC", exact: false }).click();
}

async function chooseContext(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Choose role and warehouse" }).click();
  await page.locator(".compute-popover").getByRole("button", { name: /JOURNEY_WH/ }).click();
  await expect(page.getByRole("button", { name: "Choose role and warehouse" })).toContainText("JOURNEY_WH");
  await chooseNamespace(page, "JOURNEY_DB");
}

async function rows(page: Page, expected: string[][]): Promise<void> {
  const body = page.locator('[data-role="dock"] tbody tr');
  await expect(body).toHaveCount(expected.length);
  for (const [index, values] of expected.entries()) {
    // The first cell is the grid's row number.
    await expect(body.nth(index).locator("td")).toHaveText([String(index + 1), ...values]);
  }
}

test("learning journey: CSV → stream → procedure/task → dynamic table → roles", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/");
  await login(page, "ADMIN", "admin");

  await test.step("Create the lab and choose its context", async () => {
    await execute(page, sqlFile("01-bootstrap.sql"), true, true);
    await chooseContext(page);
    await chapter(page, "02-objects.sql");
    await execute(page, "SELECT answer FROM scratch_users");
    await rows(page, [["42"]]);
  });

  await test.step("Upload the shared CSV through the object explorer", async () => {
    await page.getByRole("button", { name: "Refresh objects" }).click();
    const explorer = page.locator('[data-role="sidebar"]');
    await explorer.getByText("JOURNEY_DB", { exact: true }).click();
    await explorer.getByText("PUBLIC", { exact: true }).click();
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "Upload file to USERS_STAGE" }).click();
    await (await chooser).setFiles(fileURLToPath(new URL("users.csv", journey)));
    await expect(explorer).toContainText("users.csv uploaded");
    await chapter(page, "03-load.sql");
    await rows(page, [["3"]]);
    await execute(page, "SELECT COUNT(*) FROM users_stream");
    await rows(page, [["3"]]);
  });

  await test.step("Materialize and process changes", async () => {
    const expected = [
      [["3"]], [["0"]], [["EU", "2"], ["US", "1"]],
      [["1"]], [["4"]], [["0"]], [["3"]],
      [["EU", "2"], ["US", "2"]], [["2"]], [["4"]],
    ];
    await chapter(page, "04-pipeline.sql", true, async (sql) => {
      if (/SELECT (?:COUNT\(\*\)|SUM\(users\)|region, users)/i.test(sql)) {
        const result = expected.shift();
        expect(result).toBeDefined();
        await rows(page, result!);
      }
    });
    expect(expected).toHaveLength(0);
    await rows(page, [["4"]]);
    await execute(page, "SELECT COUNT(*) FROM users_stream");
    await rows(page, [["0"]]);
    await execute(page, "SELECT region, users FROM users_by_region ORDER BY region");
    await rows(page, [["EU", "2"], ["US", "2"]]);
    await execute(page, "SELECT COUNT(*) FROM procedure_log");
    await rows(page, [["3"]]);
    await execute(page, "SELECT 'admin-private-check'");
    await rows(page, [["admin-private-check"]]);
  });

  await test.step("Reader isolation, denied writes and denied role", async () => {
    await logout(page);
    await login(page, "JOURNEY_ALICE", "learn-alice");
    await expect(page.locator('[data-role="dock"]')).toContainText("Run a statement");
    await chooseContext(page);
    await chapter(page, "05-reader.sql");
    await rows(page, [["1", "Ada", "EU"], ["2", "Grace", "US"], ["3", "Linus", "EU"], ["4", "Barbara", "US"]]);
    await chapter(page, "06-denied.sql", false);
    await page.getByRole("button", { name: "Choose role and warehouse" }).click();
    await expect(page.locator(".compute-popover").getByRole("button", { name: /JOURNEY_WRITER/ })).toHaveCount(0);
    await page.getByRole("button", { name: "Choose role and warehouse" }).click();
    await page.getByRole("button", { name: "History", exact: true }).click();
    const history = page.locator('[data-view-pane="history"]');
    await expect(history.locator("tbody tr")).not.toHaveCount(0);
    await expect(history).not.toContainText("admin-private-check");
    await page.getByRole("button", { name: "Worksheets", exact: true }).click();
  });

  await test.step("Writer grants and role downgrade", async () => {
    await logout(page);
    await login(page, "JOURNEY_BOB", "learn-bob");
    await expect(page.locator('[data-role="dock"]')).toContainText("Run a statement");
    await chooseContext(page);
    await chapter(page, "07-writer.sql");
    await rows(page, [["4"]]);
    await page.getByRole("button", { name: "Choose role and warehouse" }).click();
    await page.locator(".compute-popover").getByRole("button", { name: "JOURNEY_READER", exact: true }).click();
    await expect(page.getByRole("button", { name: "Choose role and warehouse" })).toContainText("JOURNEY_READER");
    await execute(page, "INSERT INTO source_users VALUES (99, 'Not allowed', 'US')", false);
  });

  await test.step("Clean up with the original administrator", async () => {
    await logout(page);
    await login(page, "ADMIN", "admin");
    await chooseContext(page);
    await chapter(page, "08-cleanup.sql");
    await chooseNamespace(page, "TEST_DB");
    await chapter(page, "09-finish.sql");
    await page.getByRole("button", { name: "Refresh objects" }).click();
    await expect(page.locator('[data-role="sidebar"]')).not.toContainText("JOURNEY_DB");
    await logout(page);
  });
  expect(errors).toEqual([]);
});
