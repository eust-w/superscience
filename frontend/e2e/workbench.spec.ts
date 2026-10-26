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
  await expect(composer).toHaveValue("");
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

for (const providerState of ["unconfigured", "error"] as const) {
  test(`opens local tools with an ${providerState} provider while AI remains disabled`, async ({ page, request }) => {
    const settings = await request.put("/api/settings", {
      data: providerState === "unconfigured"
        ? { defaultProvider: "", defaultModel: "" }
        : { defaultProvider: providerID, defaultModel: "unavailable-model" }
    });
    expect(settings.ok()).toBe(true);
    const streamRequests: string[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && (request.url().includes("/messages/stream") || request.url().includes("/runs"))) streamRequests.push(request.url());
    });

    await page.goto("/");
    await expect(page).toHaveURL(/\/onboarding$/);
    await page.getByRole("button", { name: "Open local tools", exact: true }).click();
    await expect(page).toHaveURL(/\/projects$/);
    await expect(page.getByText("Cloud provider").locator("..").getByText(providerState, { exact: true })).toBeVisible();
    await page.getByLabel("Project path").fill("..");
    await page.getByRole("button", { name: "Open Folder" }).click();

    await expect(page.getByPlaceholder("Ask Superscience")).toBeDisabled();
    await expect(page.getByRole("button", { name: "Send message" })).toBeDisabled();
    await expect(page.getByText("AI chat is unavailable until a provider and model pass verification.")).toBeVisible();
    await page.getByRole("button", { name: "Notebook", exact: true }).click();
    await expect(page.getByRole("button", { name: "New and run" })).toBeVisible();
    await page.getByRole("button", { name: "Science", exact: true }).click();
    await expect(page.getByLabel("Science database")).toBeEnabled();
    await page.getByRole("button", { name: "Agents", exact: true }).click();
    await page.getByPlaceholder("Research objective").fill("Review the synthetic measurements.");
    await expect(page.getByRole("button", { name: "Run", exact: true })).toBeDisabled();
    expect(streamRequests).toEqual([]);

    await page.getByRole("button", { name: "Configure AI provider" }).click();
    await expect(page).toHaveURL(/\/onboarding$/);
    const stored = await request.get("/api/settings");
    expect((await stored.json()).defaultModel).toBe(providerState === "unconfigured" ? "" : "unavailable-model");
    await page.getByRole("button", { name: "Open local tools", exact: true }).click();
    await page.reload();
    await expect(page).toHaveURL(/\/onboarding$/);
  });
}

test("does not offer local tools while the local server is unavailable", async ({ page }) => {
  await page.route("**/api/health", (route) => route.abort("connectionrefused"));
  await page.goto("/");
  await expect(page).toHaveURL(/\/onboarding$/);
  await expect(page.getByRole("button", { name: "Open local tools", exact: true })).toBeDisabled();
});

test("local tools do not wait for provider verification or lose their workspace when it fails", async ({ page }) => {
  let releaseHealth!: () => void;
  const waiting = new Promise<void>((resolve) => { releaseHealth = resolve; });
  await page.route("**/api/providers/*/health?*", async (route) => {
    await waiting;
    await route.fulfill({ json: { ok: false, status: "error", message: "Provider verification failed during local work." } });
  });
  try {
    await page.goto("/");
    await page.getByRole("button", { name: "Open local tools", exact: true }).click();
    await page.getByLabel("Project path").fill("..");
    await page.getByRole("button", { name: "Open Folder" }).click();
    await page.getByRole("button", { name: "Science", exact: true }).click();
    await expect(page.getByLabel("Science database").locator("option")).not.toHaveCount(0);

    const response = page.waitForResponse("**/api/providers/*/health?*");
    releaseHealth();
    await response;
    await expect(page).toHaveURL(/\/workspace$/);
    await expect(page.getByPlaceholder("Ask Superscience")).toBeDisabled();
    await page.getByRole("button", { name: "Configure AI provider" }).click();
    await expect(page.getByText("Provider verification failed during local work.")).toBeVisible();
  } finally {
    releaseHealth();
  }
});


test("agent runs require verification of the selected provider and model", async ({ page, request }) => {
  await request.post("/api/providers", { data: { id: "unverified-provider", kind: "openai-compatible", displayName: "Unverified Provider", baseURL: "http://127.0.0.1:4397/v1", enabled: true } });
  await page.route("**/api/providers/*/models", (route) => route.fulfill({ json: [
    { id: modelID, displayName: "Verified model" }, { id: "other-model", displayName: "Other model" }
  ] }));
  await page.goto("/");
  await page.getByLabel("Project path").fill("..");
  await page.getByRole("button", { name: "Open Folder" }).click();
  await page.getByRole("button", { name: "Agents", exact: true }).click();
  await page.getByPlaceholder("Research objective").fill("Review the synthetic measurements.");
  const run = page.getByRole("button", { name: "Run", exact: true });
  await expect(run).toBeEnabled();
  await page.getByLabel("Model", { exact: true }).selectOption("other-model");
  await expect(run).toBeDisabled();
  await page.getByLabel("Model", { exact: true }).selectOption(modelID);
  await expect(run).toBeEnabled();
  await page.getByLabel("Provider", { exact: true }).selectOption("unverified-provider");
  await expect(run).toBeDisabled();
  await page.getByLabel("Provider", { exact: true }).selectOption(providerID);
  await expect(run).toBeEnabled();
});

test("a stale startup verification cannot enable a newly selected model", async ({ page, request }) => {
  let releaseStartup!: () => void;
  const waiting = new Promise<void>((resolve) => { releaseStartup = resolve; });
  let first = true;
  await page.route("**/api/providers/*/health?*", async (route) => {
    if (first) { first = false; await waiting; }
    const isOriginal = new URL(route.request().url()).searchParams.get("model") === modelID;
    await route.fulfill({ json: { ok: isOriginal, status: isOriginal ? "ready" : "error", message: isOriginal ? "Verified fixture" : "New model verification failed." } });
  });
  await page.route("**/api/settings/credentials/test", (route) => route.fulfill({ json: { ok: true, status: "ready", message: "Protocol test credential accepted." } }));
  await page.route("**/api/settings/credentials", (route) => route.request().method() === "POST"
    ? route.fulfill({ json: { id: "test-credential", provider: providerID, label: "Test credential", redacted: "test", source: "test" } })
    : route.continue());
  try {
    await page.goto("/");
    await page.getByRole("button", { name: "Open local tools", exact: true }).click();
    await page.getByLabel("Project path").fill("..");
    await page.getByRole("button", { name: "Open Folder" }).click();
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "Settings" });
    await dialog.getByLabel("Model", { exact: true }).fill("new-unverified-model");
    await dialog.getByPlaceholder("API key").fill("test-only-no-keychain-write");
    await dialog.getByRole("button", { name: "Verify and save" }).click();
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).defaultModel).toBe("new-unverified-model");
    await expect(dialog.getByRole("button", { name: "Verify and save" })).not.toHaveClass(/spin/);
    await dialog.getByRole("button", { name: "Close settings" }).click();
    await expect(page.getByPlaceholder("Ask Superscience")).toBeDisabled();
    const staleResponse = page.waitForResponse((response) => response.url().includes("/health?model=" + modelID));
    releaseStartup();
    await staleResponse;
    await expect(page.getByPlaceholder("Ask Superscience")).toBeDisabled();
    await page.getByRole("button", { name: "Agents", exact: true }).click();
    await page.getByPlaceholder("Research objective").fill("Review the synthetic measurements.");
    await expect(page.getByRole("button", { name: "Run", exact: true })).toBeDisabled();
  } finally { releaseStartup(); }
});
