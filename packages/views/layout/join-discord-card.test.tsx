import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { JoinDiscordCard } from "./join-discord-card";

describe("JoinDiscordCard", () => {
  it("renders null after Discord removal", () => {
    const { container } = render(<JoinDiscordCard />);
    expect(container.firstChild).toBeNull();
  });
});
