import type { ConversationEvent } from "./types";

export interface ConversationEventBatcher {
  push(event: ConversationEvent): void;
  flush(): void;
  cancel(): void;
}

type FrameScheduler = (callback: FrameRequestCallback) => number;
type FrameCanceller = (handle: number) => void;
type FallbackHandle = number;
type FallbackScheduler = (
  callback: () => void,
  delayMs: number,
) => FallbackHandle;
type FallbackCanceller = (handle: FallbackHandle) => void;

const DEFAULT_FALLBACK_DELAY_MS = 100;

const defaultScheduler: FrameScheduler = (callback) =>
  requestAnimationFrame(callback);
const defaultCanceller: FrameCanceller = (handle) =>
  cancelAnimationFrame(handle);
const defaultFallbackScheduler: FallbackScheduler = (callback, delayMs) =>
  globalThis.setTimeout(callback, delayMs) as unknown as number;
const defaultFallbackCanceller: FallbackCanceller = (handle) =>
  globalThis.clearTimeout(handle);

export const createEventBatcher = (
  onFlush: (events: readonly ConversationEvent[]) => void,
  schedule: FrameScheduler = defaultScheduler,
  cancelFrame: FrameCanceller = defaultCanceller,
  scheduleFallback: FallbackScheduler = defaultFallbackScheduler,
  cancelFallback: FallbackCanceller = defaultFallbackCanceller,
  fallbackDelayMs = DEFAULT_FALLBACK_DELAY_MS,
): ConversationEventBatcher => {
  let queue: ConversationEvent[] = [];
  let frame: number | undefined;
  let fallback: FallbackHandle | undefined;
  let generation = 0;

  const drain = () => {
    if (queue.length === 0) {
      return;
    }
    const events = queue;
    queue = [];
    onFlush(events);
  };

  const clearScheduled = () => {
    if (frame !== undefined) cancelFrame(frame);
    if (fallback !== undefined) cancelFallback(fallback);
    frame = undefined;
    fallback = undefined;
  };

  const flush = () => {
    generation += 1;
    clearScheduled();
    drain();
  };

  const scheduleFlush = () => {
    const currentGeneration = ++generation;
    frame = schedule(() => {
      if (currentGeneration !== generation) return;
      if (fallback !== undefined) cancelFallback(fallback);
      frame = undefined;
      fallback = undefined;
      generation += 1;
      drain();
    });
    fallback = scheduleFallback(() => {
      if (currentGeneration !== generation) return;
      if (frame !== undefined) cancelFrame(frame);
      frame = undefined;
      fallback = undefined;
      generation += 1;
      drain();
    }, fallbackDelayMs);
  };

  return {
    push(event) {
      queue = [...queue, event];
      if (frame === undefined && fallback === undefined) scheduleFlush();
    },
    flush,
    cancel() {
      generation += 1;
      clearScheduled();
      queue = [];
    },
  };
};
