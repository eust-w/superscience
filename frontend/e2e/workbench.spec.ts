import { expect, test, type APIRequestContext } from "@playwright/test";

const providerID = "e2e-protocol-provider";
const modelID = "e2e-research-model";

async function configureProvider(request: APIRequestContext) {
  const provider = await request.post("/api/providers", {
    data: {
      id: providerID,
      kind: "openai-compatible",
      displayName: "E2E Protocol Provider",
      baseURL: "http://127.0.0.1:4397/v1",
      enabled: true,
      secretRef: "",
      capabilities: ["streaming", "tools"]
    }
  });
  expect(provider.ok(), await provider.text()).toBe(true);

  const settings = await request.put("/api/settings", {
    data: {
      defaultProvider: providerID,
      defaultModel: modelID,
      scienceNetwork: "live-only"
    }
  });
  expect(settings.ok(), await settings.text()).toBe(true);
}

test.beforeEach(async ({ request }) => {
  await configureProvider(request);
});

test("opens a verified workspace and streams a provider response", async ({ page }, testInfo) => {
  await page.goto("/");

  await expect(page).toHaveURL(/\/projects$/);
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
  await expect(page.getByText("Cloud provider").locator("..").getByText("ready")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("launcher.png"), fullPage: true });

  await page.getByLabel("Project path").fill("..");
  await page.getByRole("button", { name: "Open Folder" }).click();

  await expect(page).toHaveURL(/\/workspace$/);
  await expect(page.getByText("No pending approvals.")).toBeVisible();
  const composer = page.getByPlaceholder("Ask Superscience");
  await composer.fill("Summarize the verified evidence path.");
  await page.getByRole("button", { name: "Send message" }).click();

  await expect(page.getByText("Protocol-backed research response.")).toBeVisible();
  await expect(page.getByText("Summarize the verified evidence path.")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("workspace.png"), fullPage: true });
});

test("keeps the workbench within the minimum desktop viewport", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto("/");
  await expect(page).toHaveURL(/\/projects$/);

  const overflow = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: document.documentElement.clientWidth
  }));
  expect(overflow.documentWidth).toBeLessThanOrEqual(overflow.viewportWidth);
  await expect(page.getByRole("button", { name: "Open Folder" })).toBeVisible();

  await page.getByRole("button", { name: "Open Folder" }).click();
  await page.getByRole("button", { name: "Ecosystem" }).click();
  const ecosystemTabs = page.locator(".ecosystem-tabs");
  await expect(ecosystemTabs).toHaveCSS("height", "32px");
  await page.getByRole("button", { name: "share", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Publish" })).toBeVisible();
  await expect(page.getByText("0 artifacts are available for the current session.")).toBeVisible();
  await expect(page.getByLabel("Artifact")).toBeDisabled();
});

test("creates, disables, and deletes a persisted custom agent", async ({ page }, testInfo) => {
  await page.goto("/");
  await page.getByLabel("Project path").fill("..");
  await page.getByRole("button", { name: "Open Folder" }).click();
  await page.getByRole("button", { name: "Agents" }).click();

  await page.getByRole("button", { name: "New agent" }).click();
  await page.getByLabel("Agent name").fill("Chemistry Review");
  await page.getByLabel("Agent ID").fill("chemistry-review");
  await page.getByLabel("Agent domain").fill("chemistry");
  await page.getByLabel("Review mode").selectOption("required");
  await page.getByLabel("Agent description").fill("Reviews chemistry evidence and uncertainty.");
  await page.getByLabel("Agent system prompt").fill("Require structure-backed evidence and explicit uncertainty.");
  await page.getByRole("button", { name: "Save agent" }).click();

  const registryAgent = page.getByRole("button", { name: /^Chemistry Review chemistry/ });
  await expect(registryAgent).toBeVisible();
  await expect(page.getByLabel("Agent")).toHaveValue("chemistry-review");
  await page.screenshot({ path: testInfo.outputPath("custom-agent.png"), fullPage: true });

  await page.getByRole("button", { name: "Disable Chemistry Review" }).click();
  await expect(page.getByText("disabled", { exact: true })).toBeVisible();
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Delete Chemistry Review" }).click();
  await expect(registryAgent).toHaveCount(0);
});
