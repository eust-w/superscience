import { describe, expect, it } from "vitest";
import { resources } from "./resources";

describe("i18n resources", () => {
  it("keeps zh-CN onboarding keys in parity with English", () => {
    expect(Object.keys(resources["zh-CN"].onboarding).sort()).toEqual(Object.keys(resources.en.onboarding).sort());
    for (const value of Object.values(resources["zh-CN"].onboarding)) expect(value.trim()).not.toBe("");
  });
});
