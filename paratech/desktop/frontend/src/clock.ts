// A one-second clock for the elapsed time of a running recording (TASK-076). The page polls the core every 3 s; the
// time itself is counted here from startedAt, so it advances by one second, not by the poll step.

/** Timers the ticker uses; the browser's in the page, fakes in tests. */
export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

export const tickMs = 1000;

// startedAt has whole seconds, so the elapsed time changes at each whole second of the clock. Firing a little after
// it keeps one change per tick even when a timer is late or early by a few ms (a free 1000 ms interval can drift
// across the boundary and skip or repeat a second).
const tickSlackMs = 15;

/** Milliseconds from `now` to just after the next whole second. */
export function nextTickDelay(now: number): number {
  const into = ((now % tickMs) + tickMs) % tickMs;
  return tickMs - into + tickSlackMs;
}

/** SecondTicker calls onTick with the clock at once when turned on, then after every whole second until turned off. */
export class SecondTicker {
  private handle: unknown = undefined;

  constructor(
    private readonly onTick: (now: number) => void,
    private readonly timers: Timers,
    private readonly clock: () => number = Date.now,
  ) {}

  get active(): boolean {
    return this.handle !== undefined;
  }

  setActive(on: boolean): void {
    if (on === this.active) {
      return;
    }
    if (!on) {
      this.timers.clear(this.handle);
      this.handle = undefined;
      return;
    }
    this.tick();
  }

  private tick(): void {
    const now = this.clock();
    this.handle = this.timers.set(() => this.tick(), nextTickDelay(now));
    this.onTick(now);
  }
}
