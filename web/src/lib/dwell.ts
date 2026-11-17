// This captures visible wall time only. Server analytics remain the authority for aggregation.
export class VisibleClock {
  private start?: number;
  private wall = 0;
  resume(monotonic: number, wall: number) {
    if (this.start === undefined) {
      this.start = monotonic;
      this.wall = wall;
    }
  }
  take(monotonic: number): { seconds: number; occurredAt: string } | undefined {
    if (this.start === undefined) return undefined;
    const seconds = Math.floor(Math.max(0, monotonic - this.start) / 1000);
    if (seconds < 1) return undefined;
    const occurredAt = new Date(this.wall).toISOString();
    this.start += seconds * 1000;
    this.wall += seconds * 1000;
    return { seconds, occurredAt };
  }
  pause(monotonic: number) {
    const segment = this.take(monotonic);
    this.start = undefined;
    return segment;
  }
}
