import { describe, expect, it } from "vitest";

import { isOfficialMarketingHost } from "./public-host";

describe("isOfficialMarketingHost", () => {
  it.each(["metanicator.ai", "www.metanicator.ai", "METANICATOR.AI", "metanicator.ai."])(
    "recognizes %s as an official marketing host",
    (host) => {
      expect(isOfficialMarketingHost(host)).toBe(true);
    },
  );

  it.each(["app.metanicator.ai", "api.metanicator.ai", "localhost", "metanicator.test"])(
    "does not treat %s as the public marketing host",
    (host) => {
      expect(isOfficialMarketingHost(host)).toBe(false);
    },
  );
});
