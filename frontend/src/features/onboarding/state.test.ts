import { describe, expect, it } from "vitest";
import { deriveStartupRoute } from "./state";

describe("deriveStartupRoute", () => {
  it("blocks the workspace until a cloud provider and model are verified", () => {
    expect(
      deriveStartupRoute({
        serverStatus: "ready",
        providerId: "",
        modelId: "",
        providerStatus: "unconfigured"
      })
    ).toBe("onboarding");
  });

  it("opens the launcher only for a live ready provider", () => {
    expect(
      deriveStartupRoute({
        serverStatus: "ready",
        providerId: "openai",
        modelId: "gpt-5.4",
        providerStatus: "ready"
      })
    ).toBe("launcher");
  });

  it("keeps provider failures actionable instead of entering the workspace", () => {
    expect(
      deriveStartupRoute({
        serverStatus: "ready",
        providerId: "anthropic",
        modelId: "claude-sonnet-4.5",
        providerStatus: "error"
      })
    ).toBe("onboarding");
  });
});
