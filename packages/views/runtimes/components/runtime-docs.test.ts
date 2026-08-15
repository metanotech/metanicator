import { describe, expect, it } from "vitest";
import {
  customRuntimeDocsHref,
  daemonRuntimesDocsHref,
} from "./runtime-docs";

describe("runtime docs links", () => {
  it.each([
    ["en", "https://metanicator.ai/docs/daemon-runtimes"],
    ["zh-Hans", "https://metanicator.ai/docs/zh/daemon-runtimes"],
    ["ja", "https://metanicator.ai/docs/ja/daemon-runtimes"],
    ["ko", "https://metanicator.ai/docs/ko/daemon-runtimes"],
  ])("localizes the daemon guide for %s", (language, expected) => {
    expect(daemonRuntimesDocsHref(language)).toBe(expected);
  });

  it("adds the localized custom runtime section", () => {
    expect(customRuntimeDocsHref("zh-Hans")).toBe(
      `https://metanicator.ai/docs/zh/daemon-runtimes#${encodeURIComponent("自定义运行时配置")}`,
    );
  });
});
