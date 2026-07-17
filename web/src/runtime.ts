import { Cause, Effect, Exit, Option } from "effect"
import { ApiClient, createApiClient, type ApiClientService } from "./api/client.js"

const client = createApiClient()

/**
 * The single Effect entrypoint used by router loaders. Expected failures are
 * unwrapped before crossing into React Router so its error boundary retains
 * the tagged HTTP/network/decode error instead of an opaque FiberFailure.
 */
export const createApiRunner = (service: ApiClientService) => <A, E>(effect: Effect.Effect<A, E, ApiClientService>): Promise<A> =>
  Effect.runPromise(Effect.exit(effect.pipe(Effect.provideService(ApiClient, service)))).then((exit) => {
    if (Exit.isSuccess(exit)) return exit.value
    const failure = Cause.failureOption(exit.cause)
    if (Option.isSome(failure)) return Promise.reject(failure.value)
    return Promise.reject(new Error(Cause.pretty(exit.cause)))
  })

export const runApi = createApiRunner(client)
