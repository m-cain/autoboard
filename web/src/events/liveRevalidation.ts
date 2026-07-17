import { Effect, Fiber, Stream } from "effect"
import type { ActivityEvent } from "@autoboard/contracts"
import { createRevalidationCoalescer } from "./revalidation.js"

type Options<Timer> = {
  readonly stream: Stream.Stream<ActivityEvent>
  readonly relevant: (event: ActivityEvent) => boolean
  readonly revalidate: () => void
  readonly schedule: (work: () => void) => Timer
  readonly cancel: (timer: Timer) => void
}

export const startActivityRevalidation = <Timer,>(options: Options<Timer>): (() => void) => {
  const revalidation = createRevalidationCoalescer(options.revalidate, options.schedule, options.cancel)
  let stopped = false
  const fiber = Effect.runFork(Stream.runForEach(options.stream, (event) => Effect.sync(() => {
    if (!stopped && options.relevant(event)) revalidation.request()
  })))
  return () => {
    if (stopped) return
    stopped = true
    revalidation.dispose()
    void Effect.runPromise(Fiber.interrupt(fiber))
  }
}
