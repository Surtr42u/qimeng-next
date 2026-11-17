import { describe, expect, it } from "vitest";
import { clampOffset, focusPreservingTransform } from "../src/lib/viewer-geometry";

describe("image gestures in visual coordinates", () => {
  it("keeps an off-centre image point under the same cursor after zooming an already panned image", () => {
    const start = { scale: 2, x: 30, y: -20 };
    const after = focusPreservingTransform(start, 3, { x: 700, y: 350 }, { x: 0, y: 0 }, { w: 1000, h: 700 }, { w: 1000, h: 700 });
    expect(after).toEqual({ scale: 3, x: -55, y: -30 });
  });
  it("follows a drifting pinch midpoint without drifting the zoom focus", () => {
    const after = focusPreservingTransform({ scale: 1, x: 0, y: 0 }, 2, { x: 600, y: 400 }, { x: 40, y: -30 }, { w: 1000, h: 700 }, { w: 1000, h: 700 });
    expect(after).toEqual({ scale: 2, x: -60, y: -80 });
  });
  it("centres an image smaller than the stage and limits exposed edges after a drag", () => {
    expect(clampOffset(0.5, 200, -100, { w: 800, h: 600 }, { w: 1000, h: 700 })).toEqual({ scale: 0.5, x: 0, y: 0 });
    expect(clampOffset(2, 900, -900, { w: 800, h: 600 }, { w: 1000, h: 700 })).toEqual({ scale: 2, x: 300, y: -250 });
  });
});
