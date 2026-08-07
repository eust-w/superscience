import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { OnboardingScreen } from "./OnboardingScreen";

describe("OnboardingScreen", () => {
  it("renders provider-required setup without demo or mock choices", () => {
    const html = renderToStaticMarkup(
      <OnboardingScreen
        providers={[
          {
            id: "openai",
            kind: "openai",
            displayName: "OpenAI",
            baseURL: "https://api.openai.com/v1",
            enabled: true,
            secretRef: "",
            capabilities: ["chat", "stream", "tools"]
          }
        ]}
        initialError="Provider verification is required"
        loadModels={async () => []}
        onVerify={async () => ({ ok: true, status: "ready", message: "Verified" })}
      />
    );

    expect(html).toContain("Configure a cloud provider");
    expect(html).toContain("Verify and continue");
    expect(html.toLowerCase()).not.toContain("mock");
    expect(html.toLowerCase()).not.toContain("demo");
    expect(html.toLowerCase()).not.toContain("example");
  });

  it("prefers OpenAI when the provider registry is alphabetically ordered", () => {
    const provider = (id: string, displayName: string) => ({
      id,
      kind: id,
      displayName,
      baseURL: "",
      enabled: true,
      secretRef: "",
      capabilities: ["chat", "stream"]
    });
    const html = renderToStaticMarkup(
      <OnboardingScreen
        providers={[provider("bedrock", "AWS Bedrock"), provider("openai", "OpenAI")]}
        loadModels={async () => []}
        onVerify={async () => ({ ok: true, status: "ready", message: "Verified" })}
      />
    );
    expect(html).toContain('<option value="openai" selected="">OpenAI</option>');
  });
});
