import { describe, expect, test } from "bun:test";

import { patchMediaSource } from "./patch-emusks.mjs";

const unsafeReturn = "return { media_id: mediaId, ...finalizeData };";
const safeReturn = "return { ...finalizeData, media_id: mediaId };";

describe("patchMediaSource", () => {
  test("keeps the exact string media ID after spreading the response", () => {
    const source = `before\n  ${unsafeReturn}\nafter\n`;

    expect(patchMediaSource(source)).toBe(`before\n  ${safeReturn}\nafter\n`);
  });

  test("rejects an upstream source without the unsafe return", () => {
    expect(() => patchMediaSource("unrecognized source")).toThrow(
      "expected exactly one unsafe media ID return, found 0",
    );
  });

  test("rejects an ambiguous upstream source", () => {
    expect(() => patchMediaSource(`${unsafeReturn}\n${unsafeReturn}`)).toThrow(
      "expected exactly one unsafe media ID return, found 2",
    );
  });
});
