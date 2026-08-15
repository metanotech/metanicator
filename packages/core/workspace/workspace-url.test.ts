import { describe, expect, it } from "vitest";
import { workspaceUrlHost } from "./workspace-url";

describe("workspaceUrlHost", () => {
  it("returns the host of a full app URL", () => {
    expect(workspaceUrlHost("https://metanicator.example.com")).toBe(
      "metanicator.example.com",
    );
  });

  it("ignores scheme, path, and trailing slash", () => {
    expect(workspaceUrlHost("https://metanicator.example.com/")).toBe(
      "metanicator.example.com",
    );
    expect(workspaceUrlHost("http://metanicator.example.com/app/onboarding")).toBe(
      "metanicator.example.com",
    );
  });

  it("preserves a non-default port", () => {
    expect(workspaceUrlHost("https://my.host:3000")).toBe("my.host:3000");
  });

  it("accepts a bare host without a scheme", () => {
    expect(workspaceUrlHost("metanicator.example.com")).toBe("metanicator.example.com");
    expect(workspaceUrlHost("metanicator.example.com/path")).toBe(
      "metanicator.example.com",
    );
  });

  it("falls back to the brand host when no app URL is configured", () => {
    expect(workspaceUrlHost("")).toBe("metanicator.ai");
    expect(workspaceUrlHost("   ")).toBe("metanicator.ai");
    expect(workspaceUrlHost(null)).toBe("metanicator.ai");
    expect(workspaceUrlHost(undefined)).toBe("metanicator.ai");
  });
});
