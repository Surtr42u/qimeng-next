import { describe, expect, it } from "vitest";
import { profilePatch, rebaseProfileDraft } from "../src/lib/profile-draft";

describe("profile drafts", () => {
  it("keeps explicit clears and omits untouched fields", () => {
    expect(
      profilePatch(
        { summary: "Old", note: "Keep" },
        { summary: "", note: "Keep" },
      ),
    ).toEqual({ summary: "" });
    expect(
      profilePatch(
        {
          accounts: [
            {
              platform: "Example",
              identifier: "x",
              url: "",
              publishingUrl: "",
            },
          ],
        },
        { accounts: [] },
      ),
    ).toEqual({ accounts: [] });
    expect(profilePatch({}, { summary: "" })).toEqual({});
  });
  it("rebases only the user changes and adopts concurrently changed untouched fields", () => {
    expect(
      rebaseProfileDraft(
        { summary: "Before", note: "Before note" },
        { summary: "Draft", note: "Before note" },
        { summary: "Remote", note: "New remote note" },
      ),
    ).toEqual({ summary: "Draft", note: "New remote note" });
  });
});
